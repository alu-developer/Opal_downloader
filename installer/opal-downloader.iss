; opal-downloader-setup.exe installer script
;
; Built with Inno Setup 6 (https://jrsoftware.org/isinfo.php), per the
; decision in docs/installer-plan.md Section 2.
;
; What this script does (docs/installer-plan.md Sections 2-4, 9 task 2):
;   - Installs opal-downloader.exe, config.example.yaml, and LICENSE to a
;     per-user install directory (no admin rights required).
;   - Bundles a pre-staged copy of Playwright's Chromium cache into
;     %LOCALAPPDATA%\ms-playwright, matching the pinned playwright-go
;     version (v0.6100.0 as of this writing - see go.mod), instead of
;     fetching it over the network at install time (Section 3, revised
;     2026-07-09).
;   - Does NOT collect config.yaml fields (download path, course patterns,
;     browser profile, etc.) - that's deferred entirely to the GUI's
;     existing first-run settings page (Section 4). The only installer-
;     specific choices are the install directory and whether to create
;     shortcuts, both stock Inno Setup wizard pages.
;   - Post-install [Run] step launches "opal-downloader.exe gui". A
;     "opal-downloader.exe setup" fallback only runs if the bundled
;     Chromium cache isn't found at its expected path at install time (e.g.
;     a version mismatch against what playwright-go expects at runtime) -
;     see NeedsPlaywrightSetup below.
;
; NOTE: this script does not stage the Chromium cache itself - that's a
; separate task's job (a build step producing a known local directory).
; For local development/testing, stage it manually by copying the browser
; folder(s) matching go.mod's playwright-go version out of your own
; %LOCALAPPDATA%\ms-playwright, e.g.:
;
;   mkdir installer\chromium-cache
;   xcopy /E /I "%LOCALAPPDATA%\ms-playwright\chromium-1228" installer\chromium-cache\chromium-1228
;   xcopy /E /I "%LOCALAPPDATA%\ms-playwright\chromium_headless_shell-1228" installer\chromium-cache\chromium_headless_shell-1228
;
; Both "chromium-<rev>" AND "chromium_headless_shell-<rev>" are required -
; opal-downloader never launches Firefox/WebKit so ffmpeg-*/firefox-*/
; webkit-*/winldd-* don't need to be staged, but internal/scraper/session.go
; launches Chromium with Headless:true for list/sync session reuse and
; Headless:false for interactive login (see CLAUDE.md "Session reuse"),
; and playwright-go's headless launch path resolves to the separate
; "chrome-headless-shell" executable under chromium_headless_shell-<rev>,
; not chrome.exe under chromium-<rev> - confirmed live: a headless launch
; against a chromium-cache staged with only chromium-<rev> fails with
; "Executable doesn't exist at ...chromium_headless_shell-*\...\
; chrome-headless-shell.exe". Both folders together added roughly 680MB
; uncompressed (~290MB compressed into the resulting setup.exe) when
; verified against playwright-go v0.6100.0 / Chromium revision 1228.
;
; Then compile with:
;   iscc installer\opal-downloader.iss
; or, to point at a different staging directory:
;   iscc /DChromiumSrcDir=C:\path\to\chromium-cache installer\opal-downloader.iss

#define MyAppName "Opal Downloader"
#define MyAppVersion "0.1.0"
#define MyAppPublisher "Opal Downloader Project"
#define MyAppURL "https://github.com/alu-developer/Opal_downloader"
#define MyAppExeName "opal-downloader.exe"

; Root of the repo checkout, relative to this script (installer/..).
#define RepoRoot SourcePath + "..\"

; Directory containing a staged copy of the Playwright Chromium cache,
; expected to contain one or more "chromium-<rev>" (and optionally
; "chromium_headless_shell-<rev>") subfolders, exactly as they appear
; under %LOCALAPPDATA%\ms-playwright. Override at compile time with
; /DChromiumSrcDir=<path> if staged elsewhere.
#ifndef ChromiumSrcDir
  #define ChromiumSrcDir SourcePath + "chromium-cache"
#endif

[Setup]
; Fixed AppId (do not regenerate) so re-running the installer over an
; existing install upgrades in place per docs/installer-plan.md Section 7.
AppId={{8E49E886-9962-49FD-A0E3-26E45521B2C8}
AppName={#MyAppName}
AppVersion={#MyAppVersion}
AppPublisher={#MyAppPublisher}
AppPublisherURL={#MyAppURL}
AppSupportURL={#MyAppURL}
AppUpdatesURL={#MyAppURL}
; Per-user, no-admin install (low-friction for a non-technical student
; machine, no UAC prompt) - see docs/installer-plan.md's ease-of-use
; framing in CLAUDE.md.
DefaultDirName={localappdata}\Programs\{#MyAppName}
DefaultGroupName={#MyAppName}
PrivilegesRequired=lowest
DisableProgramGroupPage=yes
OutputDir={#SourcePath}output
OutputBaseFilename=opal-downloader-setup
Compression=lzma2/normal
SolidCompression=yes
WizardStyle=modern
; No code signing yet (docs/installer-plan.md Section 6) - ship unsigned
; for v1, SmartScreen workaround documented separately.
UninstallDisplayIcon={app}\{#MyAppExeName}
ArchitecturesInstallIn64BitMode=x64compatible

[Languages]
Name: "english"; MessagesFile: "compiler:Default.isl"

[Tasks]
Name: "desktopicon"; Description: "Create a &desktop shortcut"; GroupDescription: "Additional shortcuts:"; Flags: unchecked

[Files]
Source: "{#RepoRoot}opal-downloader.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#RepoRoot}config.example.yaml"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#RepoRoot}LICENSE"; DestDir: "{app}"; Flags: ignoreversion
; GUI templates/static assets are compiled directly into
; opal-downloader.exe (html/template string literals in
; cmd/opal-downloader/root.go and internal/gui/*.go, no on-disk
; templates/ or static/ directory exists in this repo) - nothing
; additional to bundle for those beyond the .exe above.
; skipifsourcedoesntexist lets this script compile cleanly even when
; ChromiumSrcDir hasn't been staged (e.g. a contributor compiling the
; script without having copied a Chromium cache first) - the resulting
; installer just won't bundle Chromium in that case, and
; NeedsPlaywrightSetup below will fall back to "opal-downloader.exe setup"
; post-install.
Source: "{#ChromiumSrcDir}\*"; DestDir: "{localappdata}\ms-playwright"; Flags: ignoreversion recursesubdirs createallsubdirs uninsneveruninstall skipifsourcedoesntexist

[Icons]
Name: "{group}\{#MyAppName}"; Filename: "{app}\{#MyAppExeName}"; Parameters: "gui"
Name: "{group}\Uninstall {#MyAppName}"; Filename: "{uninstallexe}"
Name: "{autodesktop}\{#MyAppName}"; Filename: "{app}\{#MyAppExeName}"; Parameters: "gui"; Tasks: desktopicon

[Run]
; Fallback: only runs "setup" (Playwright driver install, which today
; shells out to "go run ...playwright install" - see runSetup in
; cmd/opal-downloader/root.go - so this fallback currently still requires
; a Go toolchain on PATH; fixing that is a separate task, see
; docs/installer-plan.md Section 9 task 1) if the bundled Chromium cache
; wasn't detected at its expected path after install.
Filename: "{app}\{#MyAppExeName}"; Parameters: "setup"; StatusMsg: "Bundled Chromium not found - attempting to install Playwright browsers (requires internet and Go)..."; Flags: runhidden skipifsilent; Check: NeedsPlaywrightSetup
; Primary post-install action: launch the GUI in the user's browser.
Filename: "{app}\{#MyAppExeName}"; Parameters: "gui"; Description: "Launch {#MyAppName}"; Flags: postinstall shellexec skipifsilent nowait

[Code]
function NeedsPlaywrightSetup: Boolean;
var
  FindRec: TFindRec;
  Found: Boolean;
begin
  Found := False;
  if FindFirst(ExpandConstant('{localappdata}\ms-playwright\chromium-*'), FindRec) then
  begin
    Found := True;
    FindClose(FindRec);
  end;
  Result := not Found;
end;
