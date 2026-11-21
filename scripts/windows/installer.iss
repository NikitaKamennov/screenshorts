; ScreenShooter Pro: offline, current-user setup. No downloads or runtime setup.
#ifndef PayloadRoot
  #error PayloadRoot is required
#endif
#ifndef InstallerOutput
  #error InstallerOutput is required
#endif
#ifndef InstallerName
  #error InstallerName is required
#endif
#ifndef AppVersion
  #error AppVersion is required
#endif
#ifndef FileVersion
  #error FileVersion is required
#endif

[Setup]
AppId={{71FD6640-79D2-4E1D-90E2-C4875579CC72}
AppName=ScreenShooter Pro
AppVersion={#AppVersion}
VersionInfoVersion={#FileVersion}
AppPublisher=ScreenShooter Pro
DefaultDirName={localappdata}\Programs\ScreenShooterPro
UsePreviousAppDir=yes
PrivilegesRequired=lowest
ArchitecturesAllowed=x64os
ArchitecturesInstallIn64BitMode=x64os
MinVersion=10.0.19045
DisableProgramGroupPage=yes
DisableDirPage=no
UninstallDisplayIcon={app}\ScreenShooterPro.exe
AppMutex=wails-app-skrinpolka-local-desktop-v1sim
SetupMutex=ScreenShooterProSetup-v1
CloseApplications=no
RestartApplications=no
AlwaysRestart=no
RestartIfNeededByRun=no
LicenseFile={#PayloadRoot}\LICENSE
InfoBeforeFile={#PayloadRoot}\README.txt
OutputDir={#InstallerOutput}
OutputBaseFilename={#InstallerName}
Compression=lzma2/normal
SolidCompression=yes
LZMAUseSeparateProcess=yes
LZMANumBlockThreads=2
WizardStyle=modern
UninstallLogMode=append

[Languages]
Name: "russian"; MessagesFile: "compiler:Languages\Russian.isl"
Name: "english"; MessagesFile: "compiler:Default.isl"

[Tasks]
Name: "desktopicon"; Description: "{cm:CreateDesktopIcon}"; Flags: unchecked

[CustomMessages]
russian.AppRunning=ScreenShooter Pro работает. Выбери «Выйти» в трее и повтори установку.
english.AppRunning=ScreenShooter Pro is running. Choose Exit in its tray menu and retry.
russian.LocalFolder=Выбери папку на локальном NTFS без junction и символических ссылок.
english.LocalFolder=Choose a local NTFS folder without junctions or symbolic links.
russian.ForeignFolder=В папке есть другие файлы. Выбери пустую папку или прежнюю установку ScreenShooter Pro.
english.ForeignFolder=Choose an empty folder or the previous ScreenShooter Pro installation. This folder contains other files.
russian.LinkedFolder=В папке установки есть ссылки или слишком много файлов. Она не изменена.
english.LinkedFolder=The installation folder contains links or too many files. It was not changed.
russian.InvalidInventory=Не удалось прочитать перечень прежних файлов установки. Папка не изменена.
english.InvalidInventory=The previous installation inventory cannot be read or is invalid. The folder was not changed.
russian.UninstallStopped=В папке установки есть ссылки или отсутствует её маркер. Удаление остановлено.
english.UninstallStopped=The installation folder contains links or its marker is missing. Uninstall was stopped.

[Files]
Source: "{#PayloadRoot}\*"; DestDir: "{app}"; Flags: ignoreversion recursesubdirs createallsubdirs
Source: "{#SourcePath}\installer-root.txt"; DestDir: "{app}"; DestName: "screenshorts-install-v1.txt"; Flags: ignoreversion

[Icons]
Name: "{userprograms}\ScreenShooter Pro"; Filename: "{app}\ScreenShooterPro.exe"; WorkingDir: "{app}"
Name: "{userdesktop}\ScreenShooter Pro"; Filename: "{app}\ScreenShooterPro.exe"; WorkingDir: "{app}"; Tasks: desktopicon

; No [Run], [InstallDelete] or [UninstallDelete]. The uninstaller uses its file
; log and removes directories only when empty; settings and libraries are not
; installation files. Obsolete, unchanged payload files are cleaned below.
[Code]
const
  InstallMarker = 'screenshorts-windows-install-v1';
  AppMutexName = 'wails-app-skrinpolka-local-desktop-v1sim';
var
  PreviousFiles: TArrayOfString;

function GetFileAttributesW(Name: String): LongWord;
  external 'GetFileAttributesW@kernel32.dll stdcall';
function GetDriveTypeW(Root: String): LongWord;
  external 'GetDriveTypeW@kernel32.dll stdcall';
function GetVolumeInformationW(Root, VolumeName: String; VolumeSize: LongWord;
  var Serial, MaxComponent, Flags: LongWord; FileSystem: String; FileSystemSize: LongWord): Boolean;
  external 'GetVolumeInformationW@kernel32.dll stdcall';

function PlainAncestors(Path: String): Boolean;
var
  Attr: LongWord;
  Parent: String;
begin
  Result := False;
  Path := RemoveBackslashUnlessRoot(ExpandFileName(Path));
  repeat
    Attr := GetFileAttributesW(Path);
    if (Attr <> $FFFFFFFF) and ((Attr and FILE_ATTRIBUTE_REPARSE_POINT) <> 0) then Exit;
    Parent := ExtractFileDir(Path);
    if (Parent = '') or (Parent = Path) then Break;
    Path := Parent;
  until False;
  Result := True;
end;

function PlainTree(Path: String; var Count: Integer; Depth: Integer): Boolean;
var
  Entry: TFindRec;
begin
  Result := False;
  if Depth > 40 then Exit;
  if FindFirst(AddBackslash(Path) + '*', Entry) then begin
    try
      repeat
        if (Entry.Name <> '.') and (Entry.Name <> '..') then begin
          Count := Count + 1;
          if (Count > 30000) or ((Entry.Attributes and FILE_ATTRIBUTE_REPARSE_POINT) <> 0) then Exit;
          if (Entry.Attributes and FILE_ATTRIBUTE_DIRECTORY) <> 0 then
            if not PlainTree(AddBackslash(Path) + Entry.Name, Count, Depth + 1) then Exit;
        end;
      until not FindNext(Entry);
    finally
      FindClose(Entry);
    end;
  end;
  Result := True;
end;

function LocalNTFS(Path: String): Boolean;
var
  Drive, FileSystem: String;
  Serial, MaxComponent, Flags: LongWord;
begin
  Result := False;
  Drive := ExtractFileDrive(Path);
  if (Length(Drive) <> 2) or (Drive[2] <> ':') then Exit;
  Drive := AddBackslash(Drive);
  if (GetDriveTypeW(Drive) <> 2) and (GetDriveTypeW(Drive) <> 3) then Exit;
  FileSystem := StringOfChar(#0, 32);
  if not GetVolumeInformationW(Drive, '', 0, Serial, MaxComponent, Flags, FileSystem, 32) then Exit;
  Result := CompareText(Copy(FileSystem, 1, 4), 'NTFS') = 0;
end;

function OwnedDirectory(Path: String): Boolean;
var
  Marker: AnsiString;
  MarkerPath: String;
  MarkerSize: Int64;
  Entry: TFindRec;
  HasFiles: Boolean;
begin
  Result := True;
  if not DirExists(Path) then Exit;
  HasFiles := False;
  if FindFirst(AddBackslash(Path) + '*', Entry) then begin
    try
      repeat
        if (Entry.Name <> '.') and (Entry.Name <> '..') then HasFiles := True;
      until not FindNext(Entry);
    finally
      FindClose(Entry);
    end;
  end;
  if not HasFiles then Exit;
  MarkerPath := AddBackslash(Path) + 'screenshorts-install-v1.txt';
  Result := FileSize64(MarkerPath, MarkerSize);
  if Result then Result := (MarkerSize > 0) and (MarkerSize < 128);
  if not Result then Exit;
  Result := LoadStringFromFile(MarkerPath, Marker);
  if Result then Result := Trim(String(Marker)) = InstallMarker;
end;

function ValidInventoryLine(Line: String; var Relative, Hash: String): Boolean;
var
  I, J: Integer;
  Piece, Rest, Stem, Top: String;
begin
  Result := False;
  if (Length(Line) < 67) or (Length(Line) > 512) then Exit;
  if Copy(Line, 65, 2) <> '  ' then Exit;
  Hash := LowerCase(Copy(Line, 1, 64));
  for I := 1 to 64 do if Pos(Hash[I], '0123456789abcdef') = 0 then Exit;
  Relative := Copy(Line, 67, Length(Line));
  StringChangeEx(Relative, '/', '\', True);
  if (Relative = '') or (Relative[1] = '\') or (Pos(':', Relative) <> 0) then Exit;
  I := Pos('\', Relative);
  if I = 0 then begin
    Top := LowerCase(Relative);
    if (Top <> 'screenshooterpro.exe') and (Top <> 'screenshortspro.exe') and (Top <> 'license') and (Top <> 'readme.txt') and
      (Top <> 'components-receipt.json') and (Top <> 'runtime-downloads.json') and
      (Top <> 'release-manifest.json') then Exit;
  end else begin
    Top := LowerCase(Copy(Relative, 1, I - 1));
    if (Top <> 'config') and (Top <> 'tools') and (Top <> 'runtime') and
      (Top <> 'workers') and (Top <> 'models') and (Top <> 'licenses') then Exit;
  end;
  Rest := Relative;
  repeat
    I := Pos('\', Rest);
    if I = 0 then Piece := Rest else Piece := Copy(Rest, 1, I - 1);
    if (Piece = '') or (Piece = '.') or (Piece = '..') then Exit;
    if (Piece[Length(Piece)] = '.') or (Piece[Length(Piece)] = ' ') then Exit;
    if PathHasInvalidCharacters(Piece, False) then Exit;
    for J := 1 to Length(Piece) do if Ord(Piece[J]) < 32 then Exit;
    J := Pos('.', Piece);
    if J = 0 then Stem := UpperCase(Piece) else Stem := UpperCase(Copy(Piece, 1, J - 1));
    if (Stem = 'CON') or (Stem = 'PRN') or (Stem = 'AUX') or (Stem = 'NUL') then Exit;
    if (Length(Stem) = 4) and ((Copy(Stem, 1, 3) = 'COM') or (Copy(Stem, 1, 3) = 'LPT')) then
      if Pos(Stem[4], '123456789') <> 0 then Exit;
    if I = 0 then Break;
    Delete(Rest, 1, I);
  until False;
  Result := True;
end;

function PrepareToInstall(var NeedsRestart: Boolean): String;
var
  Path, Relative, Hash, Inventory: String;
  Count, I: Integer;
  Size: Int64;
begin
  Result := '';
  SetArrayLength(PreviousFiles, 0);
  if CheckForMutexes(AppMutexName) then begin
    Result := CustomMessage('AppRunning');
    Exit;
  end;
  Path := ExpandConstant('{app}');
  if not LocalNTFS(Path) or not PlainAncestors(Path) then begin
    Result := CustomMessage('LocalFolder');
    Exit;
  end;
  if not OwnedDirectory(Path) then begin
    Result := CustomMessage('ForeignFolder');
    Exit;
  end;
  Count := 0;
  if DirExists(Path) and not PlainTree(Path, Count, 0) then begin
    Result := CustomMessage('LinkedFolder');
    Exit;
  end;
  Inventory := AddBackslash(Path) + 'SHA256SUMS.txt';
  if FileExists(Inventory) then begin
    if not FileSize64(Inventory, Size) or (Size > 4194304) then begin
      Result := CustomMessage('InvalidInventory');
      Exit;
    end;
    if not LoadStringsFromFile(Inventory, PreviousFiles) then begin
      Result := CustomMessage('InvalidInventory');
      Exit;
    end;
    if GetArrayLength(PreviousFiles) > 30000 then begin
      Result := CustomMessage('InvalidInventory');
      Exit;
    end;
    for I := 0 to GetArrayLength(PreviousFiles) - 1 do begin
      if not ValidInventoryLine(PreviousFiles[I], Relative, Hash) then begin
        Result := CustomMessage('InvalidInventory');
        Exit;
      end;
    end;
  end;
end;

procedure CurStepChanged(CurStep: TSetupStep);
var
  CurrentFiles: TArrayOfString;
  CurrentNames: TStringList;
  Path, Relative, Hash, Target, Parent: String;
  I: Integer;
begin
  if (CurStep <> ssPostInstall) or (GetArrayLength(PreviousFiles) = 0) then Exit;
  Path := ExpandConstant('{app}');
  if not LoadStringsFromFile(AddBackslash(Path) + 'SHA256SUMS.txt', CurrentFiles) then Exit;
  CurrentNames := TStringList.Create;
  try
    for I := 0 to GetArrayLength(CurrentFiles) - 1 do begin
      if not ValidInventoryLine(CurrentFiles[I], Relative, Hash) then Exit;
      CurrentNames.Add(LowerCase(Relative));
    end;
    for I := 0 to GetArrayLength(PreviousFiles) - 1 do begin
      if not ValidInventoryLine(PreviousFiles[I], Relative, Hash) then Exit;
      if CurrentNames.IndexOf(LowerCase(Relative)) < 0 then begin
        Target := AddBackslash(Path) + Relative;
        if PlainAncestors(Target) and FileExists(Target) then begin
          try
            if CompareText(GetSHA256OfFile(Target), Hash) = 0 then begin
              if DeleteFile(Target) then begin
                Parent := ExtractFileDir(Target);
                while (CompareText(Parent, Path) <> 0) and (Length(Parent) > Length(Path)) do begin
                  if not RemoveDir(Parent) then Break;
                  Parent := ExtractFileDir(Parent);
                end;
              end;
            end;
          except
            Log('An obsolete file was retained: ' + Target);
          end;
        end;
      end;
    end;
  finally
    CurrentNames.Free;
  end;
end;

function InitializeUninstall: Boolean;
var
  Path: String;
  Count: Integer;
begin
  Path := ExpandConstant('{app}');
  Count := 0;
  Result := PlainAncestors(Path) and OwnedDirectory(Path);
  if Result then Result := PlainTree(Path, Count, 0);
  if not Result then
    SuppressibleMsgBox(CustomMessage('UninstallStopped'), mbError, MB_OK, IDOK);
end;
