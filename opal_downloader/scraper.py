from __future__ import annotations

import asyncio
from dataclasses import dataclass
from pathlib import Path
from typing import Optional

from playwright.async_api import async_playwright, Browser, Page, BrowserContext


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
    Web scraper for OPAL using Playwright.
    Requires manual login (supports 2FA via TU-Fast).
    """

    def __init__(self, opal_url: str = "https://bildungsportal.sachsen.de/opal/") -> None:
        self.opal_url = opal_url.rstrip("/")
        self.browser: Optional[Browser] = None
        self.context: Optional[BrowserContext] = None
        self.page: Optional[Page] = None

    async def _launch_browser(self) -> None:
        """Launch browser and create context."""
        playwright = await async_playwright().start()
        self.browser = await playwright.chromium.launch(headless=False)
        self.context = await self.browser.new_context()
        self.page = await self.context.new_page()

    async def _close_browser(self) -> None:
        """Close browser."""
        if self.page:
            await self.page.close()
        if self.context:
            await self.context.close()
        if self.browser:
            await self.browser.close()

    async def login_and_get_courses(self, course_filter: Optional[list[str]] = None) -> list[RemoteFile]:
        """
        Open browser, wait for manual login, then scrape courses and return file list.
        
        Args:
            course_filter: List of course name patterns to include (fnmatch patterns).
                          If None or ["*"], all courses are included.
        
        Returns:
            List of RemoteFile objects representing all files in matched courses.
        """
        if not self.page:
            await self._launch_browser()

        if not self.page:
            raise RuntimeError("Failed to initialize browser")

        # Navigate to OPAL
        print(f"Opening OPAL at {self.opal_url}")
        print("Please log in manually (including 2FA if needed). The scraper will continue once logged in...")
        await self.page.goto(self.opal_url)

        # Wait for manual login by checking if we're redirected past the login page
        # We'll wait for the main course view to load
        try:
            await self.page.wait_for_selector("[data-testid='course-list'], .course-item, .folder-title", timeout=120000)
            print("Login successful!")
        except Exception:
            print("Warning: Could not detect login completion, proceeding anyway...")

        # Scrape courses
        files = await self._scrape_courses(course_filter or ["*"])
        
        return files

    async def _scrape_courses(self, course_filter: list[str]) -> list[RemoteFile]:
        """
        Scrape the courses page and extract all files matching the filter.
        """
        if not self.page:
            raise RuntimeError("No page available")

        files: list[RemoteFile] = []

        # Get all course links
        courses = await self.page.locator("a.course-title, a.folder-item, .course-name").all()
        print(f"Found {len(courses)} course elements")

        for course_elem in courses:
            try:
                course_name = await course_elem.inner_text()
                course_name = course_name.strip()

                if not self._matches_filter(course_name, course_filter):
                    print(f"  Skipped (filter): {course_name}")
                    continue

                print(f"  Processing: {course_name}")

                # Click course to open it
                await course_elem.click()
                await self.page.wait_for_load_state("networkidle")

                # Extract files from course
                course_files = await self._extract_files_from_course(course_name)
                files.extend(course_files)

                # Go back to course list
                await self.page.go_back()
                await self.page.wait_for_load_state("networkidle")

            except Exception as e:
                print(f"  Error processing course: {e}")
                continue

        return files

    async def _extract_files_from_course(self, course_name: str) -> list[RemoteFile]:
        """
        Extract all downloadable files from the current course page.
        """
        if not self.page:
            raise RuntimeError("No page available")

        files: list[RemoteFile] = []

        # Get all file links (adjust selectors based on OPAL structure)
        file_links = await self.page.locator("a[href*='download'], .file-item a, [data-action='download']").all()

        for link in file_links:
            try:
                file_name = await link.inner_text()
                file_name = file_name.strip()

                if not file_name:
                    continue

                # Get download URL
                href = await link.get_attribute("href")
                if not href:
                    continue

                # Handle relative URLs
                if href.startswith("/"):
                    url = self.opal_url.split("/opal")[0] + href
                elif not href.startswith("http"):
                    url = self.opal_url + "/" + href.lstrip("/")
                else:
                    url = href

                files.append(
                    RemoteFile(
                        name=file_name,
                        url=url,
                        course=course_name,
                        path=f"{course_name}/{file_name}",
                    )
                )
                print(f"    Found file: {file_name}")

            except Exception as e:
                print(f"    Error extracting file: {e}")
                continue

        return files

    @staticmethod
    def _matches_filter(course_name: str, patterns: list[str]) -> bool:
        """Check if course name matches any of the patterns."""
        if not patterns or patterns == ["*"]:
            return True

        from fnmatch import fnmatch
        normalized = course_name.casefold()
        for pattern in patterns:
            if fnmatch(normalized, pattern.casefold()):
                return True
        return False

    async def download_file(self, url: str, local_path: str) -> None:
        """
        Download a file from the given URL.
        Uses the current authenticated session.
        """
        if not self.page:
            raise RuntimeError("No page available")

        async with self.page.context.request as request:
            response = await request.get(url)
            if response.status == 200:
                with open(local_path, "wb") as f:
                    f.write(await response.body())
            else:
                raise RuntimeError(f"Failed to download {url}: HTTP {response.status}")

    async def close(self) -> None:
        """Close the scraper."""
        await self._close_browser()


async def scrape_opal(
    opal_url: str,
    course_filter: Optional[list[str]] = None,
) -> list[RemoteFile]:
    """
    Convenience function to scrape OPAL with automatic cleanup.
    
    Args:
        opal_url: Base URL of OPAL instance
        course_filter: Course name patterns to include
    
    Returns:
        List of RemoteFile objects
    """
    scraper = OpalScraper(opal_url)
    try:
        files = await scraper.login_and_get_courses(course_filter)
        return files
    finally:
        await scraper.close()
