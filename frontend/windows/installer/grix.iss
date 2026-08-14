; Grix Windows 安装包（Inno Setup 6）
;
; 为什么需要它：WinSparkle 下载完更新文件后只会去「执行」这个文件（源码里就是一句
; ShellExecuteEx）。给它 .exe 安装程序它就运行安装；给它 .zip，Windows 双击 zip 的
; 默认行为是弹出压缩包窗口——更新就此中止，它不会解压、更不会覆盖旧文件。所以 Windows
; 的自动更新一直是"能检测、能下载、装不上"。
; （macOS 的 Sparkle 原生支持 zip，会自动解压替换 .app，故不受影响。）
;
; 由 CI 调用：
;   ISCC.exe /DAppVersion=3.1.4 /DAppBuild=694 frontend\windows\installer\grix.iss

#ifndef AppVersion
  #error 必须传入 AppVersion（ISCC /DAppVersion=x.y.z）
#endif
#ifndef AppBuild
  #error 必须传入 AppBuild（ISCC /DAppBuild=nnn）
#endif

[Setup]
; ⛔ AppId 一旦发布就不能再改：Inno Setup 靠它认定"这是同一个应用"，
; 从而在升级时原地覆盖而不是并排装出第二份。改了它 = 所有老用户升级时多装一份。
AppId={{9F3B6C41-7D52-4E8A-9B14-2C5E8A7F0D63}
AppName=Grix
AppVersion={#AppVersion}
AppVerName=Grix {#AppVersion}
VersionInfoVersion={#AppVersion}.{#AppBuild}
AppPublisher=Grix
AppPublisherURL=https://grix.dhf.pub
DefaultDirName={code:ResolveInstallDir}
DefaultGroupName=Grix
DisableProgramGroupPage=yes
OutputBaseFilename=Grix-Windows-Setup
OutputDir=..\..\build
Compression=lzma2
SolidCompression=yes
WizardStyle=modern
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
SetupIconFile=..\runner\resources\app_icon.ico
UninstallDisplayIcon={app}\grix.exe

; 装到当前用户目录、不要管理员权限。
; 这是自动更新的关键：一旦要求管理员，WinSparkle 拉起安装程序时每次都弹 UAC，
; 用户点一次「否」更新就失败。
PrivilegesRequired=lowest

; 自动更新总是发生在 Grix 正开着的时候。不先关掉它，安装会因 exe/dll 被占用而失败。
;
; ⛔ 必须关掉 RestartManager（=no），不能用 yes 也不能用 force。原因（实机在本仓库
; Windows 机上抓到、逐条验证过）：
;   自更场景里"要更新的应用正在运行、且正是它自己（经 WinSparkle）拉起了本安装程序"。
;   只要 CloseApplications 走了 RestartManager（yes 或 force 都会走），RM 先探测到 Grix
;   在用文件、再尝试关闭；Grix 收到关闭请求要拆 Flutter 引擎 / WS 连接，来不及在 RM 超时
;   内退干净。yes 会弹模态 "安装程序无法自动关闭所有应用程序" 的 Abort/Retry/Ignore；
;   force 实测同样弹这个框（日志里是 "Shutting down applications ... (forced)" 之后照旧
;   "Some applications could not be shut down." → Message box）。而 WinSparkle 的静默更新
;   （installerArguments=/SILENT）没有任何人去点这个框：它一直卡着、最终按 Abort 回滚，
;   安装永远完不成。用户于是始终停在旧版本，每次启动 appcast 都判定"有新版本"反复提示——
;   这正是 738 装不上 740、"已是最新却一直提示升级"的根因。
;
;   =no 让安装程序完全不启用 RestartManager，也就不会弹这个框。文件占用改由下面
;   PrepareToInstall 里的 taskkill /F /IM grix.exe 强杀释放（已在实机确认能干净杀掉 Grix、
;   释放 app_links_plugin.dll 等句柄），再开始覆盖，静默更新才能真正落地。
CloseApplications=no
; RestartManager 已关，RestartApplications 无从生效；显式写 no 表明重启由 [Run] 段负责
; （带界面装走 postinstall、静默装走 WizardSilent），避免读代码时误以为还依赖 RM 重启。
RestartApplications=no

; 不弹语言选择框——自动更新时用户已经点过一次「安装更新」，不该再被问一遍语言。
ShowLanguageDialog=auto

[Languages]
; Inno Setup 自带的 Default.isl 就是英文，Languages\ 目录里并没有 English.isl（写了会编译失败）。
; 简体中文也不在官方安装包里，所以把语言文件随仓库一起带上，构建机不需要额外装任何东西。
; 来源：jrsoftware 官方翻译站收录的社区译本。
Name: "chinese"; MessagesFile: "ChineseSimplified.isl"
Name: "english"; MessagesFile: "compiler:Default.isl"

[Files]
; Flutter 的 Release 产物整个目录：grix.exe + 各 plugin dll + data/（flutter_assets 等）
Source: "..\..\build\windows\x64\runner\Release\*"; DestDir: "{app}"; \
  Flags: ignoreversion recursesubdirs createallsubdirs

[Icons]
Name: "{group}\Grix"; Filename: "{app}\grix.exe"
Name: "{group}\{cm:UninstallProgram,Grix}"; Filename: "{uninstallexe}"
Name: "{autodesktop}\Grix"; Filename: "{app}\grix.exe"; Tasks: desktopicon

[Tasks]
Name: "desktopicon"; Description: "{cm:CreateDesktopIcon}"; GroupDescription: "{cm:AdditionalIcons}"

[Run]
; 两条都要，缺一不可：
;   带界面安装（当前的自动更新走这条）——完成页的勾选框负责拉起 Grix。
Filename: "{app}\grix.exe"; Description: "{cm:LaunchProgram,Grix}"; \
  Flags: nowait postinstall skipifsilent
;   静默安装——完成页根本不存在，postinstall 不会执行。而 WinSparkle 拉起安装程序后
;   会立刻请求 Grix 退出，所以静默模式下若没有这一条，结果是「装好了、程序再也起不来」。
;   现在 appcast 还没下发 /SILENT，这条暂时不触发；但它必须先在这儿等着——
;   否则哪天有人给 appcast 补上 installerArguments，就会踩中这个雷。
Filename: "{app}\grix.exe"; Flags: nowait; Check: WizardSilent

[Code]
// ⛔ 老用户迁移：这是整个安装包最要紧的一段逻辑。
//
// 现有用户是把 zip 解压到自己随便选的目录跑的，系统里没有任何安装记录。如果安装程序
// 老老实实装到默认的 Program Files，会发生一连串糟糕的事：
//   1. Inno 的进程检测只认自己要装的那个目录，发现不了在老目录里跑着的 Grix，不会关它；
//   2. 装完把新目录的 Grix 也拉起来 → 新旧两份同时在跑，各自读写数据；
//   3. 用户桌面/任务栏的快捷方式还指着老目录，他打开的永远是老版本，
//      于是每天都被提示「有新版本」，点更新又装一遍新目录——永远出不来。
//
// 所以：安装前先找出正在运行的 grix.exe 在哪儿。只要它不是我们已经登记过的安装目录
// （即这是个 zip 老用户），就把安装目录设成它所在的那个目录，原地覆盖。
// 这样快捷方式、数据目录、用户习惯全都保住，也不会双开。
//
// 之后每次更新靠 AppId 从注册表取回上次的安装目录（UsePreviousAppDir 默认开启），
// 这段探测就不再起作用——问题一次性收敛。

var
  CachedDir: String;
  CachedDirResolved: Boolean;

// 用 PowerShell 查正在运行的 grix.exe 的完整路径，取其所在目录。
// 查不到就返回空（用户没开着 Grix，或进程已被 WinSparkle 关掉）。
function GetRunningGrixDir(): String;
var
  TmpFile: String;
  ResultCode: Integer;
  Lines: TArrayOfString;
  Cmd: String;
begin
  Result := '';
  TmpFile := ExpandConstant('{tmp}\grix_running_dir.txt');
  Cmd := '-NoProfile -NonInteractive -ExecutionPolicy Bypass -Command ' +
         '"$p = (Get-Process -Name grix -ErrorAction SilentlyContinue | ' +
         'Select-Object -First 1).Path; if ($p) { Split-Path -Parent $p | ' +
         'Out-File -Encoding ASCII -FilePath ''' + TmpFile + ''' }"';

  if not Exec('powershell.exe', Cmd, '', SW_HIDE, ewWaitUntilTerminated, ResultCode) then
    Exit;
  if not FileExists(TmpFile) then
    Exit;
  if not LoadStringsFromFile(TmpFile, Lines) then
    Exit;
  if GetArrayLength(Lines) = 0 then
    Exit;

  Result := Trim(Lines[0]);
  // 认准目录里确实有 grix.exe，避免拿到半截路径就往上装。
  if (Result <> '') and (not FileExists(AddBackslash(Result) + 'grix.exe')) then
    Result := '';
end;

// 决定装到哪里。
//
// 注意这个函数只在「没有历史安装记录」时才会被用到：Inno 的 UsePreviousAppDir 默认开启，
// 只要注册表里有上次的安装目录，它会直接沿用、根本不看 DefaultDirName。所以这里不需要
// （也不应该）自己去读注册表——手写那段既多余，AppId 里的花括号转义还很容易出错。
//
// 于是只剩两种情况：
//   Grix 正跑着但没有安装记录 → zip 老用户，原地覆盖他解压的那个目录；
//   什么都没有 → 全新安装，装到默认位置。
function ResolveInstallDir(Param: String): String;
var
  RunningDir: String;
begin
  if CachedDirResolved then
  begin
    Result := CachedDir;
    Exit;
  end;

  RunningDir := GetRunningGrixDir();
  if RunningDir <> '' then
    Result := RunningDir
  else
    Result := ExpandConstant('{autopf}\Grix');

  CachedDir := Result;
  CachedDirResolved := True;
end;

// ⛔ 覆盖前必须清干净所有 grix.exe，否则会撞上"DeleteFile 失败，错误码5，拒绝访问"。
//
// 背景：Grix 关窗口只是缩进托盘、进程并不退出（window_manager/tray_manager），仍占着
// app_links_plugin.dll 等文件；多账号多开时同一安装目录还会有多个 grix.exe 同时跑。
// 上面 [Setup] 已把 CloseApplications 设为 no（RestartManager 那条温和关闭路径在自更场景
// 会弹模态框卡死静默安装，理由见 CloseApplications 处注释），所以释放文件占用这件事**完全
// 落在这里**——PrepareToInstall 是 RM 关闭后唯一的关进程手段，不再是"兜底"。
//
// PrepareToInstall 在真正拷贝文件之前触发：强制结束所有 grix.exe，确保没有任何句柄占着
// 待覆盖的文件，否则紧接着的 DeleteFile 会撞上"错误码 5 拒绝访问"。
//
// ⛔ 只按镜像名杀、不加 /T（不杀子进程树）：自动更新时 WinSparkle 是由 grix.exe 经
// ShellExecuteEx 拉起安装程序的，Setup.exe 因此是某个 grix.exe 的子进程。若加 /T，一旦
// 那个 grix.exe 还没完全退出就被 taskkill 命中，会连同其子进程树把 Setup.exe 一起杀掉，
// 安装中途自杀。而占着 app_links_plugin.dll 等文件的就是 grix.exe 进程本身，`/IM grix.exe`
// 已全部命中，子进程树里没有别的东西持有这些 dll，/T 没有任何收益。
// 安装程序自身叫 Grix-Windows-Setup.exe、不叫 grix.exe，不会被 /IM grix.exe 误杀；
// 装完由 [Run] 段重新拉起 grix.exe。
function GrixStillRunning(): Boolean;
var
  ResultCode: Integer;
begin
  // tasklist 过滤 grix.exe；用 find 判存在：find 命中返回 0、未命中返回 1。
  // 走 cmd /C 才能用管道。Exec 成功且 find 返回 0 => 还有 grix 在跑。
  // ⚠ 探测本身启动失败（cmd 拉不起来，概率近乎为零）时保守当作"还在跑"——宁可让循环
  // 多强杀几轮再退出，也不要因为一次探测异常就误判成 grix 已清空、提前 break。
  Result := True;
  if Exec(ExpandConstant('{cmd}'),
          '/C tasklist /FI "IMAGENAME eq grix.exe" /NH | find /I "grix.exe"',
          '', SW_HIDE, ewWaitUntilTerminated, ResultCode) then
    Result := (ResultCode = 0);
end;

function PrepareToInstall(var NeedsRestart: Boolean): String;
var
  ResultCode: Integer;
  i: Integer;
begin
  // ⛔ 覆盖前把所有 grix.exe 杀干净并确认真的没了。单发一枪 taskkill 在实机不够稳：
  //   1) Grix 关窗口只是缩进托盘、进程不退，句柄仍占着 app_links_plugin.dll 等文件；
  //   2) CloseApplications=no 后没有 RestartManager 兜底，全靠这里强杀；
  //   3) 多账号多开可能有多个 grix.exe。
  // 因此循环强杀 + 每轮用 tasklist 确认，直到 grix 归零或到上限，再多等一会儿让 OS
  // 把文件句柄真正释放，避免紧接着的 DeleteFile 撞上"错误码 5 拒绝访问"。
  // 全路径 {sys}\taskkill.exe，避免依赖 PATH 解析。
  for i := 1 to 15 do
  begin
    Exec(ExpandConstant('{sys}\taskkill.exe'), '/F /IM grix.exe', '', SW_HIDE,
      ewWaitUntilTerminated, ResultCode);
    Sleep(300);
    if not GrixStillRunning() then
      Break;
  end;
  // 给 OS 一点时间把被杀进程的文件句柄真正释放掉，再开始覆盖。
  Sleep(800);
  Result := '';
end;
