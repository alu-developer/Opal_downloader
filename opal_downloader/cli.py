from __future__ import annotations

import shutil
from pathlib import Path

import click

from opal_downloader.config import load_config
from opal_downloader.sync import list_available_courses, sync_courses
from opal_downloader.webdav import OpalWebDavClient

PACKAGE_DIR = Path(__file__).resolve().parent
PROJECT_DIR = PACKAGE_DIR.parent


@click.group()
@click.version_option(package_name="opal_downloader")
def main() -> None:
    """Download and sync OPAL course files via WebDAV."""


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
    click.echo("\nEdit secrets.yaml with your WebDAV credentials, then run:")
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
    """List top-level folders available through WebDAV."""
    loaded = load_config(config_path, secrets_path)
    client = OpalWebDavClient(
        loaded.credentials.url,
        loaded.credentials.username,
        loaded.credentials.password,
    )
    client.check_connection()
    list_available_courses(client, loaded.app.roots)


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
    """Download or sync course files based on config.yaml."""
    loaded = load_config(config_path, secrets_path)
    client = OpalWebDavClient(
        loaded.credentials.url,
        loaded.credentials.username,
        loaded.credentials.password,
    )
    client.check_connection()

    click.echo(f"Download path: {loaded.app.download_path}")
    click.echo(f"Course patterns: {', '.join(loaded.app.courses)}")
    stats = sync_courses(client, loaded.app, force=force)

    click.echo(
        f"\nDone. downloaded={stats.downloaded} skipped={stats.skipped} "
        f"deleted={stats.deleted} errors={stats.errors}"
    )


if __name__ == "__main__":
    main()
