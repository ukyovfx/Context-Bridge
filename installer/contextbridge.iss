; Unsigned Phase B1 per-user installer. Signing is deferred to Phase B2.
#ifndef AppVersion
  #define AppVersion "0.0.0-dev"
#endif
#ifndef AppVersionTag
  #define AppVersionTag "v0.0.0-dev"
#endif
#ifndef SourceDir
  #define SourceDir "."
#endif
#ifndef OutputDir
  #define OutputDir "."
#endif

[Setup]
AppId={{A2F3B8CB-7A45-4FC5-9F8C-6F1C7C7B2F31}}
AppName=Context Bridge
AppVersion={#AppVersion}
AppPublisher=Context Bridge contributors
DefaultDirName={localappdata}\Programs\ContextBridge
DefaultGroupName=Context Bridge
DisableProgramGroupPage=yes
PrivilegesRequired=lowest
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
OutputDir={#OutputDir}
OutputBaseFilename=contextbridge-{#AppVersionTag}-windows-amd64-setup
Compression=lzma
SolidCompression=yes
Uninstallable=yes
ChangesEnvironment=yes
WizardStyle=modern

[Files]
Source: "{#SourceDir}\contextbridge.exe"; DestDir: "{app}"; Flags: ignoreversion restartreplace
Source: "{#SourceDir}\LICENSE"; DestDir: "{app}"; Flags: ignoreversion

[UninstallDelete]
Type: files; Name: "{app}\contextbridge.exe"
Type: files; Name: "{app}\LICENSE"
Type: files; Name: "{app}\.contextbridge-path-owned"
Type: dirifempty; Name: "{app}"

[Code]
const
  EnvironmentKey = 'Environment';
  PathValueName = 'Path';
  PathMarkerName = '.contextbridge-path-owned';

function NormalizePathEntry(Value: string): string;
begin
  Result := LowerCase(Trim(Value));
  while (Length(Result) > 0) and ((Result[Length(Result)] = '\') or (Result[Length(Result)] = '/')) do
    Delete(Result, Length(Result), 1);
end;

function PathContainsEntry(Value, Entry: string): Boolean;
var
  Remaining, Part: string;
  Separator: Integer;
begin
  Result := False;
  Remaining := Value;
  while Length(Remaining) > 0 do begin
    Separator := Pos(';', Remaining);
    if Separator = 0 then begin
      Part := Remaining;
      Remaining := '';
    end else begin
      Part := Copy(Remaining, 1, Separator - 1);
      Delete(Remaining, 1, Separator);
    end;
    if NormalizePathEntry(Part) = NormalizePathEntry(Entry) then begin
      Result := True;
      Exit;
    end;
  end;
end;

function AddUserPathEntry(Entry: string): Boolean;
var
  Existing: string;
begin
  Result := False;
  if not RegQueryStringValue(HKCU, EnvironmentKey, PathValueName, Existing) then
    Existing := '';
  if PathContainsEntry(Existing, Entry) then
    Exit;
  if Existing = '' then
    Existing := Entry
  else
    Existing := Existing + ';' + Entry;
  if RegWriteStringValue(HKCU, EnvironmentKey, PathValueName, Existing) then begin
    SaveStringToFile(ExpandConstant('{app}\' + PathMarkerName), 'Context Bridge installer-owned PATH entry', False);
    Result := True;
  end;
end;

function RemoveUserPathEntry(Entry: string): Boolean;
var
  Existing, Remaining, Part, Rebuilt: string;
  Separator: Integer;
begin
  Result := False;
  if not RegQueryStringValue(HKCU, EnvironmentKey, PathValueName, Existing) then
    Exit;
  Remaining := Existing;
  Rebuilt := '';
  while Length(Remaining) > 0 do begin
    Separator := Pos(';', Remaining);
    if Separator = 0 then begin
      Part := Remaining;
      Remaining := '';
    end else begin
      Part := Copy(Remaining, 1, Separator - 1);
      Delete(Remaining, 1, Separator);
    end;
    if NormalizePathEntry(Part) <> NormalizePathEntry(Entry) then begin
      if Rebuilt <> '' then
        Rebuilt := Rebuilt + ';';
      Rebuilt := Rebuilt + Part;
    end;
  end;
  if Rebuilt = '' then
    Result := RegDeleteValue(HKCU, EnvironmentKey, PathValueName)
  else
    Result := RegWriteStringValue(HKCU, EnvironmentKey, PathValueName, Rebuilt);
end;

procedure CurStepChanged(CurStep: TSetupStep);
begin
  if CurStep = ssPostInstall then
    AddUserPathEntry(ExpandConstant('{app}'));
end;

procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
begin
  if (CurUninstallStep = usUninstall) and FileExists(ExpandConstant('{app}\' + PathMarkerName)) then
    RemoveUserPathEntry(ExpandConstant('{app}'));
end;
