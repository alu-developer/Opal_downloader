from __future__ import annotations

import asyncio
import shutil
from pathlib import Path

import click 

from opal_downloader.config import load_config, load_credentials
from opal_downloader.sync import list_available_courses, sync_courses
from opal_downloader.scraper import OpalScraper

PACKAGE_DIR = Path(__file__).resolve().parent
PROJECT_DIR = PACKAGE_DIR.parent


@click.group()
@click.version_option(package_name="opal_downloader")
def main() -> None:
    """Download and sync OPAL course files."""


async def _run_login(scraper: OpalScraper) -> None:
    try:
        await scraper.login_with_browser()
    finally:
        await scraper.close()


async def _run_list(scraper: OpalScraper, app_config) -> None:
    try:
        await list_available_courses(scraper, app_config)
    finally:
        await scraper.close()


async def _run_sync(scraper: OpalScraper, app_config, force: bool) -> object:
    try:
        return await sync_courses(scraper, app_config, force=force)
    finally:
        await scraper.close()


@main.command("init")
@click.option(
    "--config",
    "config_path",
    type=click.Path(path_type=Path),
    default=PROJECT_DIR / "config.yaml",
    show_default=True,
)
@click.option(
    "--secrets",
    "secrets_path",
    type=click.Path(path_type=Path),
    default=PROJECT_DIR / "secrets.yaml",
    show_default=True,
)
def init_cmd(config_path: Path, secrets_path: Path) -> None:
    """Create config.yaml and secrets.yaml from the examples."""
    examples = {
        config_path: PROJECT_DIR / "config.example.yaml",
        secrets_path: PROJECT_DIR / "secrets.example.yaml",
    }
    for target, source in examples.items():
        if target.exists():
            click.echo(f"skip (exists): {target}")
            continue
        if not source.exists():
            raise click.ClickException(f"Missing example file: {source}")
        shutil.copyfile(source, target)
        click.echo(f"created: {target}")
    click.echo("\nNext steps:")
    click.echo("  1. Run: opal-downloader login")
    click.echo("  2. Edit config.yaml with your download path and course patterns")
    click.echo("  3. Run: opal-downloader sync")


@main.command("login")
@click.option(
    "--secrets",
    "secrets_path",
    type=click.Path(exists=True, dir_okay=False, path_type=Path),
    default=PROJECT_DIR / "secrets.yaml",
    show_default=True,
)
def login_cmd(secrets_path: Path) -> None:
    """
    Authenticate with OPAL and save persistent session state.
    Run this once, then sync/list can reuse the saved state.
    """
    credentials = load_credentials(secrets_path)
    scraper = OpalScraper(
        credentials.url,
        credentials.state_file,
        credentials.browser_executable,
        credentials.browser_user_data_dir,
    )

    click.echo("Opening OPAL for login...")
    click.echo("Please log in using TU-Fast (2FA is supported).")
    click.echo()
    asyncio.run(_run_login(scraper))
    click.echo()
    click.echo("✓ Login successful! Session state saved.")
    click.echo(f"Session state file: {credentials.state_file}")
    click.echo()
    click.echo("You can now run: opal-downloader sync")


@main.command("list")
@click.option(
    "--config",
    "config_path",
    type=click.Path(exists=True, dir_okay=False, path_type=Path),
    default=PROJECT_DIR / "config.yaml",
    show_default=True,
)
@click.option(
    "--secrets",
    "secrets_path",
    type=click.Path(exists=True, dir_okay=False, path_type=Path),
    default=PROJECT_DIR / "secrets.yaml",
    show_default=True,
)
def list_cmd(config_path: Path, secrets_path: Path) -> None:
    """List available courses (uses saved session state or browser login)."""
    loaded = load_config(config_path, secrets_path)
    scraper = OpalScraper(
        loaded.credentials.url,
        loaded.credentials.state_file,
        loaded.credentials.browser_executable,
        loaded.credentials.browser_user_data_dir,
    )

    asyncio.run(_run_list(scraper, loaded.app))


@main.command("sync")
@click.option(
    "--config",
    "config_path",
    type=click.Path(exists=True, dir_okay=False, path_type=Path),
    default=PROJECT_DIR / "config.yaml",
    show_default=True,
)
@click.option(
    "--secrets",
    "secrets_path",
    type=click.Path(exists=True, dir_okay=False, path_type=Path),
    default=PROJECT_DIR / "secrets.yaml",
    show_default=True,
)
@click.option("--force", is_flag=True, help="Re-download all matched files.")
def sync_cmd(config_path: Path, secrets_path: Path, force: bool) -> None:
    """Download or sync course files based on config.yaml (uses saved session state or browser login)."""
    loaded = load_config(config_path, secrets_path)
    scraper = OpalScraper(
        loaded.credentials.url,
        loaded.credentials.state_file,
        loaded.credentials.browser_executable,
        loaded.credentials.browser_user_data_dir,
    )

    click.echo(f"Download path: {loaded.app.download_path}")
    click.echo(f"Course patterns: {', '.join(loaded.app.courses)}")
    if loaded.app.default_course_folder:
        click.echo(f"Default course folder: {loaded.app.default_course_folder}")
    if loaded.app.course_folders:
        click.echo("Course folder rules:")
        for pattern, folder in loaded.app.course_folders.items():
            click.echo(f"  {pattern} -> {folder}")
    click.echo()
    stats = asyncio.run(_run_sync(scraper, loaded.app, force))

    click.echo(
        f"\nDone. downloaded={stats.downloaded} skipped={stats.skipped} "
        f"errors={stats.errors}"
    )


if __name__ == "__main__":
    main()

