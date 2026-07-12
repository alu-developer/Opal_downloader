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
; {code:GetSuggestedBrowserProfileArg} expands (via GetSuggestedBrowserProfileArg
; below) to either an empty string or a --suggested-browser-user-data-dir=...
; flag carrying the detected Brave/Chrome profile root, per
; docs/installer-plan.md Section 9 task 8 (Section 5's last bullet). This is
; informational only - see that function's doc comment for why it's safe
; under the "no auto-configuring the browser profile" constraint.
Filename: "{app}\{#MyAppExeName}"; Parameters: "gui {code:GetSuggestedBrowserProfileArg}"; Description: "Launch {#MyAppName}"; Flags: postinstall shellexec skipifsilent nowait

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

{ Brave/Chrome detection, per docs/installer-plan.md Section 5. Purely
  informational: a directory-existence check only, no writes, no config
  changes, and no attempt to install/configure Brave, Chrome, or the
  TU-Fast extension. login/sync need one of these with TU-Fast installed
  and logged in, but that setup is explicitly out of scope for this
  installer - this only tells the user so, if neither is found. }
function BrowserDetected: Boolean;
begin
  Result :=
    DirExists(ExpandConstant('{localappdata}\BraveSoftware\Brave-Browser')) or
    DirExists(ExpandConstant('{localappdata}\Google\Chrome'));
end;

{ GetSuggestedBrowserProfileArg (docs/installer-plan.md Section 9 task 8,
  Section 5's last bullet - explicitly "later, optional"): if Brave's or
  Chrome's default profile *root* ("User Data", not a specific profile
  folder inside it - matches config.example.yaml's browser_user_data_dir
  documentation) exists on disk, return a --suggested-browser-user-data-dir
  flag carrying that path for the post-install "gui" [Run] step below to
  pass through. Brave is checked first and wins if both are present, since
  TU-Fast/OPAL support has historically been documented/tested against
  Brave in this project (see CLAUDE.md).

  This is a pure directory-existence check with no side effects - same
  no-writes, no-assumptions-baked-into-config posture as BrowserDetected
  above. The GUI (internal/gui) only ever uses this value to pre-fill the
  Settings page's browser_user_data_dir *text input* when that field is
  otherwise empty; the user still has to look at it and press "Save
  settings" themselves before it becomes real config, exactly like Section
  5 requires - this installer script never writes config.yaml itself. }
function GetSuggestedBrowserProfileArg(Param: String): String;
var
  BravePath, ChromePath: String;
begin
  Result := '';
  BravePath := ExpandConstant('{localappdata}\BraveSoftware\Brave-Browser\User Data');
  ChromePath := ExpandConstant('{localappdata}\Google\Chrome\User Data');
  if DirExists(BravePath) then
    Result := '--suggested-browser-user-data-dir="' + BravePath + '"'
  else if DirExists(ChromePath) then
    Result := '--suggested-browser-user-data-dir="' + ChromePath + '"';
end;

var
  BrowserInfoPage: TWizardPage;

procedure InitializeWizard;
var
  InfoLabel: TNewStaticText;
  LinkLabel: TNewStaticText;
begin
  { Created unconditionally; ShouldSkipPage below decides at wizard-run time
    whether it's actually shown, since BrowserDetected can only be evaluated
    once the installer is running on the target machine. }
  BrowserInfoPage := CreateCustomPage(wpSelectTasks,
    'Browser Requirement', 'Brave or Chrome not found');

  InfoLabel := TNewStaticText.Create(BrowserInfoPage);
  InfoLabel.Parent := BrowserInfoPage.Surface;
  InfoLabel.Left := 0;
  InfoLabel.Top := 0;
  InfoLabel.Width := BrowserInfoPage.SurfaceWidth;
  InfoLabel.AutoSize := False;
  InfoLabel.WordWrap := True;
  InfoLabel.Caption :=
    'Opal Downloader could not find Brave or Chrome installed on this ' +
    'computer.' + #13#10#13#10 +
    'The "login" and "sync" features need Brave or Chrome, with the ' +
    'TU-Fast browser extension installed and logged in, to sign in to ' +
    'OPAL. This installer does not install or configure Brave, Chrome, ' +
    'or TU-Fast for you - that has to be done separately.' + #13#10#13#10 +
    'You can continue installing Opal Downloader now and set up the ' +
    'browser later, before you first use "login" or "sync". See the ' +
    'manual setup checklist for details:';
  InfoLabel.Height := ScaleY(170);

  LinkLabel := TNewStaticText.Create(BrowserInfoPage);
  LinkLabel.Parent := BrowserInfoPage.Surface;
  LinkLabel.Left := 0;
  LinkLabel.Top := InfoLabel.Top + InfoLabel.Height + ScaleY(8);
  LinkLabel.Width := BrowserInfoPage.SurfaceWidth;
  LinkLabel.AutoSize := False;
  LinkLabel.WordWrap := True;
  LinkLabel.Caption := '{#MyAppURL}/blob/master/docs/manual-setup-checklist.md';
end;

function ShouldSkipPage(PageID: Integer): Boolean;
begin
  { Skip this informational page entirely when either browser is present -
    proceed as normal, no page shown at all. When neither is found, the
    page shows with a plain Next button (non-blocking, not a hard gate). }
  if PageID = BrowserInfoPage.ID then
    Result := BrowserDetected
  else
    Result := False;
end;
