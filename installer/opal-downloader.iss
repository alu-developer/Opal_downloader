; opal-downloader-setup.exe installer script
;
; Built with Inno Setup 6 (https://jrsoftware.org/isinfo.php), per the
; decision in docs/installer-plan.md Section 2.
;
; What this script does (docs/installer-plan.md Sections 2-4, 9 task 2):
;   - Installs opal-downloader.exe, config.example.yaml, and LICENSE to a
;     per-user install directory (no admin rights required).
;   - Bundles a pre-staged copy of Playwright's Chromium cache into
;     {%USERPROFILE}\.opal-downloader\ms-playwright, matching the pinned
;     playwright-go version (v0.6100.0 as of this writing - see go.mod),
;     instead of fetching it over the network at install time (Section 3,
;     revised 2026-07-09). This is NOT %LOCALAPPDATA%\ms-playwright -
;     EnsurePlaywrightBrowsersPath (internal/scraper/session.go) has
;     defaulted PLAYWRIGHT_BROWSERS_PATH to the user-profile path since
;     commit b352143 (2026-07-13), to dodge an NTFS-junction failure under
;     %LOCALAPPDATA% on at least one machine. Found 2026-07-31: this script
;     had not followed that move, so a fresh install staged the bundled
;     Chromium where opal-downloader never looks, and NeedsPlaywrightSetup's
;     own probe (also pointed at %LOCALAPPDATA%) found it "present" there
;     and skipped the one fallback that would have recovered - see
;     docs/installer-plan.md's addendum for the write-up.
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
; %USERPROFILE%\.opal-downloader\ms-playwright, e.g.:
;
;   mkdir installer\chromium-cache
;   xcopy /E /I "%USERPROFILE%\.opal-downloader\ms-playwright\chromium-1228" installer\chromium-cache\chromium-1228
;   xcopy /E /I "%USERPROFILE%\.opal-downloader\ms-playwright\chromium_headless_shell-1228" installer\chromium-cache\chromium_headless_shell-1228
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
; under %USERPROFILE%\.opal-downloader\ms-playwright. Override at compile
; time with /DChromiumSrcDir=<path> if staged elsewhere.
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
Source: "{#ChromiumSrcDir}\*"; DestDir: "{%USERPROFILE}\.opal-downloader\ms-playwright"; Flags: ignoreversion recursesubdirs createallsubdirs uninsneveruninstall skipifsourcedoesntexist

[Icons]
Name: "{group}\{#MyAppName}"; Filename: "{app}\{#MyAppExeName}"; Parameters: "gui"
Name: "{group}\Uninstall {#MyAppName}"; Filename: "{uninstallexe}"
Name: "{autodesktop}\{#MyAppName}"; Filename: "{app}\{#MyAppExeName}"; Parameters: "gui"; Tasks: desktopicon

[Run]
; Fallback: only runs "setup" (Playwright browser install via playwright-go's
; own Install() API - see runSetup in cmd/opal-downloader/root.go - no Go
; toolchain needed, just network access) if the bundled Chromium cache
; wasn't detected at its expected path after install.
Filename: "{app}\{#MyAppExeName}"; Parameters: "setup"; StatusMsg: "Bundled Chromium not found - attempting to install Playwright browsers (requires internet)..."; Flags: runhidden skipifsilent; Check: NeedsPlaywrightSetup
; Primary post-install action: launch the GUI. Chromium-only login (queue
; task chromium-only-login-remove-real-browser, 2026-07-14) means
; opal-downloader never launches a real installed Brave/Chrome executable
; anymore - it always uses Playwright's bundled Chromium against its own
; dedicated login profile - so there is no detected-browser-profile flag to
; pass through here anymore (the old --suggested-browser-user-data-dir flag
; and GetSuggestedBrowserProfileArg below were removed along with it).
Filename: "{app}\{#MyAppExeName}"; Parameters: "gui"; Description: "Launch {#MyAppName}"; Flags: postinstall shellexec skipifsilent nowait

[Code]
function NeedsPlaywrightSetup: Boolean;
var
  FindRec: TFindRec;
  Found: Boolean;
begin
  Found := False;
  if FindFirst(ExpandConstant('{%USERPROFILE}\.opal-downloader\ms-playwright\chromium-*'), FindRec) then
  begin
    Found := True;
    FindClose(FindRec);
  end;
  Result := not Found;
end;

{ Chromium-only login (queue task chromium-only-login-remove-real-browser,
  2026-07-14): opal-downloader always launches Playwright's bundled Chromium
  against its own dedicated login profile now, never a real installed
  Brave/Chrome executable - so there is no longer anything for this
  installer to detect or inform the user about here. The former Brave/Chrome
  detection (BrowserDetected/GetSuggestedBrowserProfileArg) and the
  "Browser Requirement" wizard info page were removed along with Strategy 1
  (see docs/browser-profile-strategy.md's "Chromium-only login" section) -
  TU-Fast setup, if the user wants it, happens entirely inside
  opal-downloader's own dedicated profile via the GUI's /tufast-setup page
  or `opal-downloader login`, which this installer's post-install [Run] step
  already launches into. }

{ Friction-campaign finding (installer walk, 2026-08-11): uninstalling left
  ~680MB behind (the uninsneveruninstall Chromium cache above, deliberately -
  see the [Files] comment - plus the user's own config.yaml/login-profile/
  status files under %USERPROFILE%\.opal-downloader, which this installer
  never touches because it never installed them) without saying so anywhere,
  so someone uninstalling to reclaim space recovered a small fraction of what
  they expected. Not verified against a live compile/uninstall (iscc is not
  available in this environment) - the Pascal Scripting API used here
  (CurUninstallStepChanged/usPostUninstall/MsgBox) is standard Inno Setup 6,
  matching the pattern in Inno's own documentation, but flagging this as
  source-written-not-live-verified per this project's own rule against
  silently promoting an unverified fix to "done". }
procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
begin
  if CurUninstallStep = usPostUninstall then
  begin
    MsgBox(
      'Opal Downloader has been uninstalled, but two things were left behind on purpose:' + #13#10 + #13#10 +
      '- The bundled Chromium browser cache (~680 MB) at' + #13#10 +
      '  ' + ExpandConstant('{%USERPROFILE}') + '\.opal-downloader\ms-playwright' + #13#10 +
      '  so a future reinstall does not need to re-download it.' + #13#10 + #13#10 +
      '- Your login session and app settings under' + #13#10 +
      '  ' + ExpandConstant('{%USERPROFILE}') + '\.opal-downloader' + #13#10 + #13#10 +
      'Delete that folder yourself if you want to reclaim the space.',
      mbInformation, MB_OK);
  end;
end;
