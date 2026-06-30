from __future__ import annotations

import asyncio
import shutil
from pathlib import Path

import click

from opal_downloader.config import load_config
from opal_downloader.sync import list_available_courses, sync_courses
from opal_downloader.scraper import OpalScraper

PACKAGE_DIR = Path(__file__).resolve().parent
PROJECT_DIR = PACKAGE_DIR.parent


@click.group()
@click.version_option(package_name="opal_downloader")
def main() -> None:
    """Download and sync OPAL course files."""


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
    click.echo("\nEdit secrets.yaml if needed (default OPAL URL is configured).")
    click.echo("Edit config.yaml with your download path and course patterns.")
    click.echo("Then run:")
    click.echo("  opal-downloader list")
    click.echo("  opal-downloader sync")


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
    """List available courses in OPAL (requires manual login in browser)."""
    loaded = load_config(config_path, secrets_path)
    scraper = OpalScraper(loaded.credentials.url)
    
    try:
        asyncio.run(list_available_courses(scraper, loaded.app))
    finally:
        asyncio.run(scraper.close())


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
    """Download or sync course files based on config.yaml (requires manual login in browser)."""
    loaded = load_config(config_path, secrets_path)
    scraper = OpalScraper(loaded.credentials.url)
    
    try:
        click.echo(f"Download path: {loaded.app.download_path}")
        click.echo(f"Course patterns: {', '.join(loaded.app.courses)}")
        click.echo()
        stats = asyncio.run(sync_courses(scraper, loaded.app, force=force))

        click.echo(
            f"\nDone. downloaded={stats.downloaded} skipped={stats.skipped} "
            f"errors={stats.errors}"
        )
    finally:
        asyncio.run(scraper.close())


if __name__ == "__main__":
    main()

