from __future__ import annotations

from dataclasses import dataclass
from pathlib import Path
import re
from typing import Optional
from urllib.parse import parse_qsl, urlencode, urljoin, urlsplit, urlunsplit

from playwright.async_api import Browser, BrowserContext, Page, Playwright, async_playwright
from opal_downloader.config import course_matches


@dataclass(frozen=True)
class RemoteFile:
    """Represents a file found in OPAL."""
    name: str
    url: str
    course: str
    path: str  # relative path within course (e.g., "folder/subfolder/file.pdf")
    size: Optional[int] = None
    modified: Optional[str] = None


class OpalScraper:
    """
    Hybrid OPAL scraper using Playwright session persistence.

    Workflow:
    1. First run: interactive login in visible browser (works with TU-Fast/2FA).
    2. Session state is stored locally.
    3. Next runs: headless browser reuses saved session state.
    4. On session expiry: automatic fallback to interactive re-login.
    """

    def __init__(
        self,
        opal_url: str = "https://bildungsportal.sachsen.de/opal/",
        state_file: Optional[Path] = None,
        browser_executable: Optional[Path] = None,
        browser_user_data_dir: Optional[Path] = None,
    ) -> None:
        self.opal_url = opal_url.rstrip("/") + "/"
        self.state_file = state_file or Path.home() / ".opal_storage_state.json"
        self.browser_executable = browser_executable
        self.browser_user_data_dir = browser_user_data_dir
        self._playwright: Optional[Playwright] = None
        self.browser: Optional[Browser] = None
        self.context: Optional[BrowserContext] = None
        self.page: Optional[Page] = None
        self._download_candidates: dict[str, tuple[str, str, str]] = {}

    async def _launch_browser(self, *, headless: bool, use_saved_state: bool) -> None:
        """Launch browser and create context, optionally loading saved session state."""
        if self._playwright is None:
            self._playwright = await async_playwright().start()

        launch_kwargs: dict[str, object] = {"headless": headless}
        if self.browser_executable:
            launch_kwargs["executable_path"] = str(self.browser_executable)

        # Use real browser profile only for interactive login where extensions (TU-Fast)
        # are needed. For saved-session runs we use an isolated Playwright context.
        if self.browser_user_data_dir and not headless and not use_saved_state:
            # Extensions are disabled by default in Playwright Chromium.
            # Removing that arg enables installed profile extensions like TU-Fast.
            self.context = await self._playwright.chromium.launch_persistent_context(
                str(self.browser_user_data_dir),
                ignore_default_args=["--disable-extensions"],
                **launch_kwargs,
            )
            self.page = self.context.pages[0] if self.context.pages else await self.context.new_page()
            return

        self.browser = await self._playwright.chromium.launch(**launch_kwargs)
        context_kwargs: dict[str, object] = {}
        if use_saved_state and self.state_file.exists():
            context_kwargs["storage_state"] = str(self.state_file)
        self.context = await self.browser.new_context(**context_kwargs)
        self.page = await self.context.new_page()
        self.page.set_default_timeout(15000)
        self.page.set_default_navigation_timeout(20000)

    async def _close_opal_pages(self) -> None:
        if not self.context:
            return
        for page in list(self.context.pages):
            try:
                url = (page.url or "").casefold()
                if "bildungsportal.sachsen.de/opal" in url or "about:blank" in url:
                    await page.close()
            except Exception:
                pass

    async def _close_browser(self) -> None:
        """Close browser."""
        await self._close_opal_pages()
        self.page = None
        if self.context:
            try:
                await self.context.close()
            except Exception:
                pass
            self.context = None
        if self.browser:
            try:
                await self.browser.close()
            except Exception:
                pass
            self.browser = None

    async def _save_state(self) -> None:
        if not self.context:
            raise RuntimeError("No browser context available")
        self.state_file.parent.mkdir(parents=True, exist_ok=True)
        await self.context.storage_state(path=str(self.state_file))
        print(f"Saved session state to: {self.state_file}")

    async def _is_authenticated(self) -> bool:
        """Heuristic login check that works across OPAL/ILIAS pages."""
        if not self.page:
            return False
        await self.page.goto(self.opal_url, wait_until="domcontentloaded")
        page_url = self.page.url.casefold()
        if "login" in page_url or "shib" in page_url or "idp" in page_url:
            return False
        password_inputs = await self.page.locator("input[type='password']").count()
        if password_inputs > 0:
            return False
        course_candidates = await self.page.locator("a[href*='crs_'], a[href*='course'], a[href*='RepositoryEntry']").count()
        return course_candidates > 0

    async def _ensure_session(self, *, force_interactive: bool) -> None:
        """Ensure an authenticated session is available."""
        await self._close_browser()
        if not force_interactive and self.state_file.exists():
            await self._launch_browser(headless=True, use_saved_state=True)
            if await self._is_authenticated():
                print("Using saved OPAL session state")
                return
            print("Saved session state expired. Interactive login required.")
            await self._close_browser()

        await self._launch_browser(headless=False, use_saved_state=False)
        if not self.page:
            raise RuntimeError("Failed to initialize browser page")
        print(f"Opening OPAL at {self.opal_url}")
        print("Please complete login in the opened browser window (TU-Fast/2FA supported).")
        await self.page.goto(self.opal_url, wait_until="domcontentloaded")
        await self.page.wait_for_selector("a[href*='crs_'], a[href*='course'], a[href*='RepositoryEntry']", timeout=300000)
        await self._save_state()

    async def login_with_browser(self) -> None:
        """Force interactive login and persist session state."""
        await self._ensure_session(force_interactive=True)

    async def scrape_with_saved_session(self, course_filter: Optional[list[str]] = None) -> list[RemoteFile]:
        """Scrape using saved session state with automatic interactive fallback."""
        await self._ensure_session(force_interactive=False)
        return await self._scrape_courses_browser(course_filter or ["*"])

    async def _scrape_courses_browser(self, course_filter: list[str]) -> list[RemoteFile]:
        """Scrape courses using Playwright browser."""
        if not self.page:
            raise RuntimeError("No page available")

        files: list[RemoteFile] = []

        courses = await self._discover_course_links(course_filter, max_pages=12)

        print(f"Found {len(courses)} course links")

        for course_name, course_url in courses:
            try:
                if not self._matches_filter(course_name, course_filter):
                    print(f"  Skipped (filter): {course_name}")
                    continue

                print(f"  Processing: {course_name}")

                # Crawl each course page tree to discover files in nested folders/elements.
                course_files = await self._crawl_course_files(course_name, course_url)
                files.extend(course_files)

            except Exception as e:
                print(f"  Error processing course: {e}")
                continue

        return files

    async def _discover_course_links(self, course_filter: list[str], *, max_pages: int = 12) -> list[tuple[str, str]]:
        """Discover courses from stable OPAL overview pages using repository IDs."""
        if not self.page:
            raise RuntimeError("No page available")

        discovered: dict[str, tuple[str, str]] = {}
        source_pages = [
            urljoin(self.opal_url, "auth/home"),
            urljoin(self.opal_url, "auth/RepositoryEntry/resources"),
            urljoin(self.opal_url, "auth/resource/resources"),
        ]

        for source_url in source_pages:
            try:
                await self.page.goto(source_url, wait_until="domcontentloaded", timeout=20000)
                await self._wait_for_course_entries(source_url)
            except Exception:
                continue

            candidates = await self._extract_course_candidates_from_current_page()
            for candidate in candidates:
                link_target = self._extract_link_target(
                    candidate["href"],
                    candidate["onclick"],
                    candidate["dataHref"],
                    candidate["dataUrl"],
                )
                if not link_target:
                    continue

                abs_url = urljoin(self.opal_url, link_target)
                repo_id = self._extract_repository_entry_id(abs_url)
                if not repo_id:
                    continue

                if not self._looks_like_course_candidate(candidate):
                    continue

                label = self._derive_course_label(
                    candidate["title"],
                    candidate["text"],
                    candidate["cardText"],
                    repo_id,
                )
                canonical_url = urljoin(self.opal_url, f"auth/RepositoryEntry/{repo_id}")

                previous = discovered.get(repo_id)
                if not previous or previous[0].startswith("RepositoryEntry") or len(label) > len(previous[0]):
                    discovered[repo_id] = (label, canonical_url)

        # If specific patterns are configured, use a focused catalog crawl only for
        # still-missing patterns. This avoids crawling everything on normal runs.
        missing_patterns = self._get_unmatched_patterns(course_filter, list(discovered.values()))
        if missing_patterns:
            print(f"Course discovery fallback: searching catalog for {len(missing_patterns)} unmatched pattern(s)")
            extra = await self._discover_from_catalog_for_patterns(missing_patterns, max_pages=40)
            for repo_id, pair in extra.items():
                if repo_id not in discovered or discovered[repo_id][0].startswith("RepositoryEntry"):
                    discovered[repo_id] = pair

        return sorted(discovered.values(), key=lambda item: item[0].casefold())

    async def _discover_from_catalog_for_patterns(
        self,
        missing_patterns: list[str],
        *,
        max_pages: int,
    ) -> dict[str, tuple[str, str]]:
        if not self.page:
            raise RuntimeError("No page available")

        discovered: dict[str, tuple[str, str]] = {}
        visited: set[str] = set()
        queue: list[str] = [
            urljoin(self.opal_url, "auth/repository/catalog"),
            urljoin(self.opal_url, "auth/repository/catalog/3571713"),  # TU Dresden
        ]
        queued: set[str] = {self._normalize_url_for_crawl(item) for item in queue}

        while queue and len(visited) < max_pages:
            current_url = queue.pop(0)
            key = self._normalize_url_for_crawl(current_url)
            queued.discard(key)
            if key in visited:
                continue
            visited.add(key)

            try:
                await self.page.goto(current_url, wait_until="domcontentloaded", timeout=20000)
                await self._wait_for_course_entries(current_url)
            except Exception:
                continue

            candidates = await self._extract_course_candidates_from_current_page()
            for candidate in candidates:
                link_target = self._extract_link_target(
                    candidate["href"],
                    candidate["onclick"],
                    candidate["dataHref"],
                    candidate["dataUrl"],
                )
                if not link_target:
                    continue

                abs_url = urljoin(self.opal_url, link_target)
                repo_id = self._extract_repository_entry_id(abs_url)
                if not repo_id:
                    continue
                if not self._looks_like_course_candidate(candidate):
                    continue

                label = self._derive_course_label(
                    candidate["title"],
                    candidate["text"],
                    candidate["cardText"],
                    repo_id,
                )
                if not course_matches(label, missing_patterns):
                    continue

                canonical_url = urljoin(self.opal_url, f"auth/RepositoryEntry/{repo_id}")
                discovered[repo_id] = (label, canonical_url)

            # Traverse only catalog folders to keep this fallback bounded and deterministic.
            catalog_links = await self.page.evaluate(
                """
                () => Array.from(document.querySelectorAll('a[href]'))
                    .map(a => (a.getAttribute('href') || '').trim())
                    .filter(href => href.includes('/auth/repository/catalog/'))
                """
            )
            for href in catalog_links:
                abs_url = urljoin(self.opal_url, str(href))
                abs_key = self._normalize_url_for_crawl(abs_url)
                if abs_key not in visited and abs_key not in queued:
                    queue.append(abs_url)
                    queued.add(abs_key)

            still_missing = self._get_unmatched_patterns(missing_patterns, list(discovered.values()))
            if not still_missing:
                break

        return discovered

    @staticmethod
    def _get_unmatched_patterns(patterns: list[str], courses: list[tuple[str, str]]) -> list[str]:
        if not patterns or patterns == ["*"]:
            return []

        unmatched: list[str] = []
        course_names = [name for name, _ in courses]
        for pattern in patterns:
            if not any(course_matches(name, [pattern]) for name in course_names):
                unmatched.append(pattern)
        return unmatched

    async def _extract_course_candidates_from_current_page(self) -> list[dict[str, str]]:
        if not self.page:
            raise RuntimeError("No page available")

        entries = await self.page.evaluate(
            """
            () => {
                const out = [];
                for (const el of document.querySelectorAll('a[href], [onclick], [data-href], [data-url]')) {
                    const card = el.closest('li, article, .list-group-item, .dynamic-tab, .o_repository_entry, .o_repoentry, .o_infoPanel, .o_card') || el.parentElement;
                    out.push({
                        href: (el.getAttribute('href') || '').trim(),
                        onclick: (el.getAttribute('onclick') || '').trim(),
                        dataHref: (el.getAttribute('data-href') || '').trim(),
                        dataUrl: (el.getAttribute('data-url') || '').trim(),
                        text: (el.textContent || '').trim(),
                        title: (el.getAttribute('title') || '').trim(),
                        cardClass: ((card && card.getAttribute('class')) || '').trim(),
                        cardText: ((card && card.textContent) || '').trim(),
                    });
                }
                return out;
            }
            """
        )
        return [dict(item) for item in entries if isinstance(item, dict)]

    async def _wait_for_course_entries(self, page_url: str) -> None:
        if not self.page:
            raise RuntimeError("No page available")

        selectors = [
            "a[href*='/RepositoryEntry/']",
            "a[href*='/CourseNode/']",
            ".list-group-item a[href]",
            ".dynamic-tab a[href]",
        ]

        # On resources/home pages OPAL often lazy-loads the course list after base nav links.
        # Wait specifically for course-like anchors instead of generic a[href].
        is_resources_like = any(token in page_url.casefold() for token in ("repositoryentry/resources", "/auth/home", "resource/resources"))
        timeout_ms = 12000 if is_resources_like else 5000

        for selector in selectors:
            try:
                await self.page.wait_for_selector(selector, timeout=timeout_ms)
                await self.page.wait_for_timeout(800)
                return
            except Exception:
                continue

        await self.page.wait_for_timeout(1500)

    async def _crawl_course_files(self, course_name: str, start_url: str) -> list[RemoteFile]:
        if not self.page:
            raise RuntimeError("No page available")

        files: list[RemoteFile] = []
        file_seen: set[str] = set()
        visited_pages: set[str] = set()
        queue: list[str] = [start_url]
        queued: set[str] = {self._normalize_url_for_crawl(start_url)}
        max_pages = 12
        start_repo_id = self._extract_repository_entry_id(start_url)

        while queue and len(visited_pages) < max_pages:
            current_url = queue.pop(0)
            current_key = self._normalize_url_for_crawl(current_url)
            queued.discard(current_key)
            if current_key in visited_pages:
                continue
            visited_pages.add(current_key)

            if len(visited_pages) % 10 == 0:
                print(f"    Crawl progress: visited={len(visited_pages)} queued={len(queue)} files={len(files)}")

            try:
                await self.page.goto(current_url, wait_until="domcontentloaded", timeout=20000)
                try:
                    await self.page.wait_for_selector("a[href], [onclick], [data-href], [data-url]", timeout=4000)
                except Exception:
                    await self.page.wait_for_timeout(1200)
            except Exception:
                continue

            links = await self.page.evaluate(
                """
                () => {
                    const out = [];
                    for (const el of document.querySelectorAll('a[href], [onclick], [data-href], [data-url]')) {
                        out.push({
                            href: (el.getAttribute('href') || '').trim(),
                            text: (el.textContent || '').trim(),
                            title: (el.getAttribute('title') || '').trim(),
                            onclick: (el.getAttribute('onclick') || '').trim(),
                            dataHref: (el.getAttribute('data-href') || '').trim(),
                            dataUrl: (el.getAttribute('data-url') || '').trim(),
                        });
                    }
                    return out;
                }
                """
            )

            for item in links:
                href_raw = str(item.get("href", "")).strip()
                onclick_raw = str(item.get("onclick", "")).strip()
                data_href_raw = str(item.get("dataHref", "")).strip()
                data_url_raw = str(item.get("dataUrl", "")).strip()
                text = self._pick_label(str(item.get("title", "")).strip(), str(item.get("text", "")).strip())

                link_target = self._extract_link_target(href_raw, onclick_raw, data_href_raw, data_url_raw)
                if not link_target:
                    continue

                abs_url = urljoin(self.opal_url, link_target)
                if "bildungsportal.sachsen.de/opal" not in abs_url.casefold():
                    continue

                repo_id = self._extract_repository_entry_id(abs_url)
                if start_repo_id and repo_id and repo_id != start_repo_id:
                    continue

                if self._looks_like_download_link(link_target, text):
                    cleaned_name = self._sanitize_filename(text or Path(abs_url).name or "download")
                    cleaned_course = self._sanitize_filename(course_name)
                    file_key = f"{cleaned_course}|{abs_url}"
                    if file_key in file_seen:
                        continue
                    file_seen.add(file_key)
                    self._download_candidates[abs_url] = (current_url, text, link_target)
                    files.append(
                        RemoteFile(
                            name=cleaned_name,
                            url=abs_url,
                            course=cleaned_course,
                            path=f"{cleaned_course}/{cleaned_name}",
                        )
                    )
                    print(f"    Detected file: {cleaned_name}")
                    continue

                if self._looks_like_browse_link(link_target, text):
                    abs_key = self._normalize_url_for_crawl(abs_url)
                    if abs_key not in visited_pages and abs_key not in queued:
                        queue.append(abs_url)
                        queued.add(abs_key)

        print(f"    Crawled {len(visited_pages)} pages, found {len(files)} files")
        return files

    @staticmethod
    def _looks_like_download_link(href: str, text: str) -> bool:
        href_l = href.casefold()
        text_l = text.casefold()
        if "tabelle herunterladen" in text_l:
            return False
        if any(skip in href_l for skip in ("/login", "shibboleth", "logout", "cmd=edit", "cmd=delete")):
            return False
        # Exclude obvious navigation targets that are not files.
        if any(nav in href_l for nav in ("target=fold_", "target=crs_", "target=grp_", "cmd=view", "cmdclass=ilrepositorygui")):
            return False
        # OPAL/ILIAS file links appear in multiple forms depending on page type.
        if any(token in href_l for token in (
            "sendfile",
            "cmd=sendfile",
            "target=file_",
            "file_",
            "cmd=download",
            "cmdclass=ilobjfilegui",
            "cmdclass=ilobjfoldergui",
            "cmd=export",
            "/download",
            "getfile",
        )):
            return True
        return bool(re.search(r"\.(pdf|zip|doc|docx|ppt|pptx|xls|xlsx|txt|csv|ipynb|py|java|c|cpp|jpg|jpeg|png)$", text_l))

    @staticmethod
    def _looks_like_browse_link(href: str, text: str) -> bool:
        href_l = href.casefold()
        text_l = text.casefold()
        if any(skip in text_l for skip in (
            "neuigkeiten",
            "ankündigungen",
            "forum",
            "kalender",
            "gehe zu seite",
            "aktuelle seite",
            "vorherige seite",
            "nächste seite",
        )):
            return False
        if any(skip in href_l for skip in (
            "/login",
            "shibboleth",
            "logout",
            "cmd=edit",
            "cmd=delete",
            "target=file_",
            "downloadtablecontainer",
            "&antiCache=",
            "-pager-",
        )):
            return False
        return any(
            token in href_l
            for token in (
                "target=fold_",
                "target=grp_",
                "target=crs_",
                "goto.php?target=fold_",
                "goto.php?target=grp_",
                "goto.php?target=crs_",
                "/coursenode/",
                "/repositoryentry/",
                "baseclass=ilmembershipoverviewgui",
                "baseclass=ildashboardgui",
                "mycourses",
                "membership",
            )
        )

    @staticmethod
    def _looks_like_course_link(href: str, text: str) -> bool:
        href_l = href.casefold()
        text_l = text.casefold()
        if len(text_l) < 3:
            return False
        if text_l.startswith("http://") or text_l.startswith("https://"):
            return False
        if any(skip in text_l for skip in ("aktuelle seite", "vorherige seite", "nächste seite", "next page", "previous page")):
            return False
        if any(token in href_l for token in ("target=file_", "cmd=download", "cmd=sendfile", "sendfile", "/download")):
            return False
        if any(skip in text_l for skip in ("neuigkeiten", "ankündigungen", "materialien", "übungseinschreibung", "forum", "kalender")):
            return False
        if any(token in href_l for token in (
            "target=crs_",
            "target=grp_",
            "goto.php?target=crs_",
            "goto.php?target=grp_",
            "/auth/repositoryentry/",
            "/auth/coursenode/",
        )):
            return True
        return False

    @staticmethod
    def _is_course_label(text: str) -> bool:
        text_l = text.casefold().strip()
        if len(text_l) < 3:
            return False
        return not any(
            bad in text_l
            for bad in (
                "gehe zu",
                "aktuelle seite",
                "vorherige seite",
                "nächste seite",
                "tabelle herunterladen",
                "ankündigungen",
                "materialien",
                "übungseinschreibung",
                "vorlesung",
                "übungsblätter",
                "probeklausur",
            )
        )

    @classmethod
    def _looks_like_course_candidate(cls, candidate: dict[str, str]) -> bool:
        href = candidate.get("href", "")
        text = candidate.get("text", "")
        title = candidate.get("title", "")
        card_text = candidate.get("cardText", "")
        card_class = candidate.get("cardClass", "").casefold()

        combined = " ".join((text, title, card_text)).casefold()
        if not cls._looks_like_course_link(href, title or text):
            return False
        if any(skip in combined for skip in ("gehe zu seite", "tabelle herunterladen", "forum", "kalender", "ankündigungen")):
            return False

        if "dynamic-tab" in card_class:
            return True
        if "list-group-item" in card_class and "verantwortliche" in combined:
            return True
        if "zuletzt angesehen" in combined:
            return True
        return cls._is_course_label(title or text)

    @classmethod
    def _derive_course_label(cls, title: str, text: str, card_text: str, repo_id: str) -> str:
        for raw in (title, text, card_text):
            cleaned = re.sub(r"\s+", " ", raw).strip()
            if not cleaned:
                continue
            cleaned = re.sub(r"^Als Favorit markieren\s+", "", cleaned, flags=re.IGNORECASE).strip()
            cleaned = re.sub(r"\bVerantwortliche\(r\):.*$", "", cleaned, flags=re.IGNORECASE).strip(" -,")
            cleaned = re.sub(r"\bZuletzt angesehen:.*$", "", cleaned, flags=re.IGNORECASE).strip(" -,")
            cleaned = re.sub(r"\bAufrufe:.*$", "", cleaned, flags=re.IGNORECASE).strip(" -,")
            if cls._is_course_label(cleaned):
                return cleaned
        return f"RepositoryEntry {repo_id}"

    @staticmethod
    def _pick_label(title_text: str, visible_text: str) -> str:
        if title_text and len(title_text) > len(visible_text):
            return title_text
        return visible_text

    @staticmethod
    def _extract_link_target(href: str, onclick: str, data_href: str, data_url: str) -> str | None:
        for candidate in (href, data_href, data_url):
            c = candidate.strip()
            if c and c != "#" and not c.casefold().startswith("javascript:"):
                return c

        if onclick:
            # Common patterns: location.href='...'; window.open("...")
            match = re.search(r"(?:location\.href|window\.open)\s*\(\s*['\"]([^'\"]+)['\"]", onclick)
            if match:
                return match.group(1)
            match = re.search(r"location\.href\s*=\s*['\"]([^'\"]+)['\"]", onclick)
            if match:
                return match.group(1)
            if "goto.php" in onclick:
                match = re.search(r"(\/[^'\"]*goto\.php[^'\"]*)", onclick)
                if match:
                    return match.group(1)
        return None

    @staticmethod
    def _sanitize_filename(name: str) -> str:
        cleaned = re.sub(r'[<>:"/\\|?*\x00-\x1F]', "_", name.strip())
        cleaned = re.sub(r"\s+", " ", cleaned)
        cleaned = cleaned.rstrip(". ")
        if cleaned.upper() in {"CON", "PRN", "AUX", "NUL", "COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9", "LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9"}:
            cleaned = f"_{cleaned}"
        return cleaned or "unnamed"

    @staticmethod
    def _extract_repository_entry_id(url: str) -> str | None:
        match = re.search(r"/RepositoryEntry/(\d+)", url, flags=re.IGNORECASE)
        return match.group(1) if match else None

    @staticmethod
    def _normalize_url_for_crawl(url: str) -> str:
        split = urlsplit(url)
        query_items = parse_qsl(split.query, keep_blank_values=False)
        keep_keys = {
            "target",
            "ref_id",
            "cmd",
            "cmdClass",
            "baseClass",
            "cmdNode",
            "node_id",
            "crs_next_sess",
        }
        filtered = [(k, v) for k, v in query_items if k in keep_keys]
        filtered.sort(key=lambda item: (item[0], item[1]))
        normalized_query = urlencode(filtered)
        return urlunsplit((split.scheme, split.netloc, split.path, normalized_query, ""))

    @staticmethod
    def _matches_filter(course_name: str, patterns: list[str]) -> bool:
        """Check if course name matches any of the patterns."""
        return course_matches(course_name, patterns)

    async def download_file(self, url: str, local_path: str) -> None:
        """
        Download a file from the given URL.
        Uses the authenticated Playwright request context.
        """
        if not self.context:
            raise RuntimeError("No authenticated browser context available")
        response = await self.context.request.get(url)
        if response.status == 200:
            headers = {k.casefold(): v for k, v in response.headers.items()}
            content_type = headers.get("content-type", "").casefold()
            body = await response.body()
            if "text/html" not in content_type:
                if local_path.casefold().endswith(".pdf") and not body.startswith(b"%PDF"):
                    raise RuntimeError("Downloaded payload is not a valid PDF")
                with open(local_path, "wb") as handle:
                    handle.write(body)
                return

        await self._download_file_via_browser(url, local_path)

    async def _download_file_via_browser(self, url: str, local_path: str) -> None:
        if not self.page:
            raise RuntimeError("No browser page available for download fallback")

        # Try direct navigation as download trigger first.
        try:
            async with self.page.expect_download(timeout=15000) as download_info:
                await self.page.goto(url, wait_until="domcontentloaded", timeout=20000)
            download = await download_info.value
            await download.save_as(local_path)
            return
        except Exception:
            pass

        source_url, link_text, link_target = self._download_candidates.get(url, ("", "", ""))
        if not source_url:
            raise RuntimeError("Response is HTML, not a direct file download")

        await self.page.goto(source_url, wait_until="domcontentloaded", timeout=20000)

        # Prefer href-based click, then text-based fallback.
        target_fragment = link_target.strip()
        if target_fragment.startswith("/"):
            target_fragment = target_fragment
        elif target_fragment.startswith("http"):
            split = urlsplit(target_fragment)
            target_fragment = split.path

        try:
            if target_fragment:
                async with self.page.expect_download(timeout=15000) as download_info:
                    await self.page.locator(f"a[href*='{target_fragment}']").first.click(timeout=5000)
                download = await download_info.value
                await download.save_as(local_path)
                return
        except Exception:
            pass

        if link_text:
            async with self.page.expect_download(timeout=15000) as download_info:
                await self.page.get_by_text(link_text, exact=False).first.click(timeout=5000)
            download = await download_info.value
            await download.save_as(local_path)
            return

        raise RuntimeError("Response is HTML, browser fallback click did not find downloadable link")

    async def close(self) -> None:
        """Close the scraper and cleanup resources."""
        await self._close_browser()
        if self._playwright:
            try:
                await self._playwright.stop()
            except Exception:
                pass
            self._playwright = None

