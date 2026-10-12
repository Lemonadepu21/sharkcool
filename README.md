# SharkCool / BRB02 独立驱动 · Linux & Windows

## 1.12.1

原生概览页新增屏幕、灯效开关，分别读取设备 C1 / 11 状态；点击直接生效，读回确认后更新，并与屏幕/灯效页面保持一致。断线、未知状态和屏保传输期间禁用控制，失败保留已读状态，不重放操作。应用图标改为用户选择的鲨鲨酱「夏日笑脸」，详见 [素材说明](packaging/ASSET-NOTICE.md)。

Windows x64 提供 USB HID 和 BLE 通信后端、独立 Qt 窗口、无控制台启动器和可选桌面快捷方式。Windows 构建会生成包含运行库与快捷方式的安装 EXE，以及完整解压后可双击 `SharkCool.exe` 的便携 ZIP；构建产物位于 GitHub Actions，暂未发布 Release。Windows 版尚待散热器真机验证；CPU 温度、非 NVIDIA 显卡温度、主机音频/输入采集暂未支持。具体步骤、限制和构建说明见 [Windows 使用说明](packaging/windows/README-Windows.md)。

GitHub Actions 在 Ubuntu / Windows 上编译与测试，Windows 自动携带 Qt 运行库生成便携 ZIP。手动构建：`./scripts/build-windows.ps1 -QtRoot C:/Qt/6.8.3/msvc2022_64`，需要 VS 2022 C++ 工具、Go、CMake、Python、Node 和 Qt Widgets/Network/Svg/Bluetooth/Test。运行测试不等同于硬件验证。

从用户提供的黑鲨装备箱安装包中分析 BRB02 通信模块，并实现独立用户态驱动。独立 Go 包位于 `brb02/`，Linux 通信不依赖 Windows EXE、DLL、Qt 或 Wine；Windows 通信通过项目自己的 HID/BLE 组件，桌面使用 Qt 控件。中文本地界面和命令行调用同一个驱动。

**BLE 和 USB 均已完成本机 BRB02 通信验证。** BLE 已读取固件 `3.0.3`、真实心跳和散热配置，并验证固定转速、RGB 与 LCD 开关。USB-IN 使用原配 C-to-C 数据线连接电脑后枚举 `e2b7:7001`，已通过 hidraw 收到真实心跳，并读回固件、散热配置、RGB 和启动选项。USB 单色/彩色切换已通过八字节配置读回；其他控制写入及仅用 USB-IN 供电仍待单独验证；本次测试另接充电器到电源口。

已补齐六种自主 RGB 灯效、亮度、单色和速度设置，并通过 BLE 验证完整配置读回。已加入三行 LCD 参数布局和 Linux 系统信息推送，通过 BLE 验证布局成功响应和持续发送。现已恢复四个固定转速槽、四个四点曲线槽的读写及 CPU/GPU 温度来源切换；配置槽保存与当前生效分别验证。新增播放音频灯效与指定 RGB 配置查询，BLE 已验证静音流推送和中断后灯效恢复。已新增 Wayland 输入活动采集和响应灯效 CLI，以及按程序匹配的场景 CLI；温控曲线完整启用仍待补齐，完整驱动目标尚未完成。

## 两种连接方式

两种通道使用同一 BRB02 协议，各自处理传输封装：

| 通道 | Linux 实现 | 连接条件 |
| --- | --- | --- |
| USB | hidraw，VID/PID `e2b7:7001`；输出为零报告 ID + 64 字节 | 设备实际提供 USB 数据接口，且当前用户拥有读写权限 |
| BLE | BlueZ 系统 D-Bus；服务 AE30、通知 AE04、写入 AE10；只发送实际协议帧 | 系统已配对，设备连接稳定，控制 GATT 服务完成解析 |

`--transport auto` 默认选择当前可用接口：已连接且控制服务就绪的 BLE，或已枚举的 USB；两者均未就绪时尝试连接已配对的 BLE。连接中断后关闭旧传输，间隔约 2 秒重新选择。`--transport ble`、`--transport usb` 可分别固定通道。运行中不会向蓝牙媒体按键 HID 发送 USB 控制报告。

## 直接运行

```sh
./dist/sharkcool doctor
./dist/sharkcool gui --transport ble
./dist/sharkcool gui --transport usb
./dist/sharkcool                         # 自动选择可用通道
```

默认打开 Qt 原生控件应用，无浏览器或 WebView 依赖。`gui --browser` 显式打开可选网页入口，`gui --no-browser` 只运行后台。默认驱动地址 `http://127.0.0.1:17865`。Ctrl+C 停止服务；关闭原生窗口不停止后台程序，再次点击桌面快捷方式可打开窗口。同一用户只允许一个实际设备控制进程；同一驱动地址只有一个原生窗口实例。控制服务仅监听回环 IP，并校验修改请求的会话令牌和来源。

无硬件演示：`./dist/sharkcool gui --demo`。演示不会读取或写入设备，不支持生成查询/开关的伪造响应，不修改自启配置。

## 断线恢复

1.10.1 保留单一读循环和串行命令，并补强失联处理：USB 读写失败、BLE 断开/服务失效会清理状态、关闭旧连接并自动重试；连续 10 秒收不到数据也会重新建立连接，重试间隔约 2 秒，单次连接尝试最多 8 秒。屏保正常传输会持续接收分片/确认，不会因整张图耗时较长而触发这个空闲超时。

重连后清除旧转速和确认状态，界面重新读取当前散热、RGB 和启动选项，不自动重放断开前未确认的写入。已开启的系统信息推送会继续尝试。命令等待串行队列时同样遵守调用者的超时，避免长时间传图拖住其他请求；连接已断开时不能把队列中的旧响应认作成功。屏保传输中断会报告失败并释放控制按钮，不跨连接接续旧分片；原图备份保留，重连后可点击“恢复原图”。

测试覆盖静默失联、短写入、首次重连失败后继续重试、等待超时不写入、屏保中断及界面重新读取。真实 USB 连接上注入三次读链路中断，四次打开/四次关闭均完成，每次恢复真实心跳，固件、RGB、散热、启动选项与屏保元数据保持一致。结果保存在 `analysis/brb02-usb-recovery-validation.json`。这不是物理拔插测试；当前蓝牙的真机恢复仍待验证。自动重连不能修复操作系统配对密钥失效或 USB 数据线本身不可用的问题。

## 双系统蓝牙配对

Windows 与 Linux 可以轮流使用散热器。同一蓝牙适配器在两个系统下通常使用同一个地址，但各自保存配对密钥；在另一系统重新配对可能使旧密钥失效。本机已确认两边的 LTK、IRK、EDIV、Rand 不一致，与先前认证失败相符。这与散热器最多可保存多少台不同设备的配对记录是两个问题，BRB02 的配对数量目前没有确证。若要切换系统后无需重新配对，需要核对并同步这台设备的配对信息，不能只套用传统蓝牙 LinkKey 步骤。同步时备份原记录，停止蓝牙服务后更新指定设备，保留认证标志与其他设备记录；Windows AuthReq 是请求参数，不能直接当作已完成认证的标志。驱动运行及发布包不读取、修改或导出配对密钥，本机修复资料不随发布包分发。可参考 [BlueZ 存储格式](https://github.com/bluez/bluez/blob/master/doc/settings-storage.txt) 与 [双系统配对问题说明](https://github.com/mgedmin/btdualboot)。

## 命令行

```sh
./dist/sharkcool status --transport ble --timeout 12s
./dist/sharkcool linklog --timeout 20s   # 只读记录 BlueZ 的断连原因
./dist/sharkcool watch --transport ble
./dist/sharkcool query --transport ble firmware
./dist/sharkcool query --transport ble cooling
./dist/sharkcool query --transport ble rgb-switch
./dist/sharkcool switch --transport ble rgb off
./dist/sharkcool switch --transport ble lcd on
./dist/sharkcool speed --transport ble 1800  # 1200～3600 RPM
./dist/sharkcool rgb --transport ble --effect static --color '#00ff48' --brightness 80
./dist/sharkcool rgb --transport ble --effect breathing --lighting-speed 70
./dist/sharkcool rgb --transport usb --color-mode multi
./dist/sharkcool mode --transport ble 2
./dist/sharkcool mode --transport usb 3
./dist/sharkcool status --transport usb --device /dev/hidraw3
```

选项必须放在位置参数前。`--address 蓝牙MAC` 指定已配对的 BRB02；不会自动创建或删除配对。`--timeout 12s` 控制连接/心跳等待时限，命令发送后的响应另有同等时限。

`query` 支持 `firmware`、`device`（官方 03 状态原始响应）、`cooling`、`rgb`、`rgb-switch`、`fan-switch`、`lcd-switch`、`source`、`screen-info`，返回命令号和 `payloadHex`，不把未经真机验证的字段解释为设备状态。`switch` 支持 `fan`、`rgb`、`lcd` 的 `on/off`，等待同命令响应；收到响应不等于已观察到实际状态变化。未收到响应会明确报错。

1.10.1 图形界面把固定转速、快捷选择和自定义数值合并为同一个风扇编辑器；温控曲线可在同一区域切换，配置槽和温度来源位于高级设置。连接及重连后自动读取当前散热配置、RGB 灯效和设备启动选项，不需要手动读取；读取失败自动重试，并禁用尚未识别设置的应用按钮。当前灯效自动选中，只修改用户编辑的 RGB 字段，避免把彩色效果意外改为单色。彩色/单色按设备读回自动选中，切换彩色时不会提交残留的单色值；设置与系统信息折叠收纳，常用区域只保留控件和必要状态。转速不再显示更新时间。CPU/GPU 温度来自本机传感器，每五秒采集，未连接散热器时也显示；演示模式的本机温度是真实值，风扇读数与控制结果是模拟值。`mode 2/3` 是 Linux 的固定转速预设，分别设置 2912/3534 RPM；不是设备固件的模式编号。`speed` 支持本应用限定的 1200～3600 RPM，不声称这是设备的完整速度范围。

RGB 灯效支持 `flow`（彩色流动）、`cycle`（颜色循环）、`breathing`（呼吸）、`static`（常亮）、`blinking`（闪烁）、`refresh`（刷新）。`--brightness` 和 `--lighting-speed` 范围均为 0～100；速度越高，对应官方协议间隔越小，常亮没有速度参数。仅修改指定字段，其余参数先从设备读取并保留；`--color-mode single|multi` 切换颜色模式；指定颜色时切换为单色，彩色模式保留设备原有配色字段。网页也可读取当前设置、选择灯效后应用。音频同步和响应灯效通过独立 CLI 采集主机事件，使用方法见下文。

每次 RGB 写入后查询并比较全部八字节配置，只有完全一致才报告 `confirmed:true`；不能只凭 ACK 声称成功。六种灯效已在固件 3.0.3 上通过 BLE 验证读回；USB 单色→彩色切换也已验证，其他七字节均保留。实际视觉效果尚未逐一核对。测试已恢复原有刷新灯效、80% 亮度和彩色设置。

转速配置发送后，驱动查询当前配置，只有读回目标匹配才标记 `acknowledged:true`。实测 3534 目标下心跳约 3630 RPM，实际转速存在偏差并会逐步变化。测试结束已恢复原先 1200 RPM 目标及开启的 RGB/LCD。

实机还确认：发送帧需要和校验；固件的查询响应/ACK 尾字节却不总符合此算法。接收端依据官方程序按帧头与长度解码，另通过 `checksumValid` 报告和校验结果，不丢弃合法的固件响应。

## 散热配置槽与温度来源

```sh
./dist/sharkcool cooling --transport ble
./dist/sharkcool cooling --transport ble --slot 1 --curve
./dist/sharkcool cooling --transport ble --slot 1 --source cpu --points 20:1200,40:1800,60:2600,80:3600
./dist/sharkcool cooling --transport ble --slot 1 --source cpu --rpm 1800
./dist/sharkcool source --transport ble gpu
```

四个固定槽和四个曲线槽分别保存配置。曲线是四组 `温度:RPM`，温度 0～100 °C 严格递增，转速字段使用官方 1200～4000 范围；固定槽也可用 `--rpm` 保存。`--slot 0` 或未指定 slot 时只查询当前生效配置；写入需显式指定 1～4。`--curve` 用于读取曲线槽；`--points` 写曲线，`--rpm` 写固定槽，两者不能同时使用。

保存后先用 26 读回配置槽，再用 25 查询当前生效配置。结果 `confirmed:true` 表示配置槽完全匹配；`active:true` 才表示当前配置也匹配；曲线还需要一秒后再次匹配，以排除固件短暂回显后的回退。这个结果不是无限期温控运行的保证。若只保存而未生效，界面明确说明，不能把它当成温控已运行。温控需要可用主机温度持续推送；完整模式启用流程仍在恢复，当前测试写入曲线后仍读回固定配置。

`source cpu/gpu` 发送 22 并用 23 读回确认；不带参数则查询来源。CPU/GPU 为 0/1，来源变更后当前散热配置的来源字段也随之改变。界面自动读取生效配置，高级设置支持载入所选槽；应用时同时提交温度来源和配置槽。高级固定槽与旧 `speed` 的区别：前者保存指定槽并单独报告是否生效，后者使用已验证的固定槽 1，要求当前读回匹配，本应用旧入口限定 1200～3600。

本机固件 3.0.3 已读出全部八个配置槽，并验证曲线槽写入/完整读回与 CPU/GPU 来源切换。测试备份并恢复原曲线、CPU 来源和当前 1200 RPM 配置，未改变其他配置槽。

## 设备启动选项

```sh
./dist/sharkcool device-options --transport ble
./dist/sharkcool device-options --transport ble --smart-start-stop=true
./dist/sharkcool device-options --transport ble --power-on-start=false
```

智能启停与上电自启动保存于设备；它们与 Linux 登录自启分别设置。只传其中一个选项时先查询再修改，保留另一个开关。命令 02 写两个布尔字节，命令 03 必须完整读回匹配，ACK 不算确认。网页需先读取设备选项，才能应用修改。已在 BLE 真机验证智能启停开/关并恢复原值；上电自启动字段已读取和保留，断电后的实际启动行为尚待验证。

SDK 场景标志接口 `SetSceneMode(ctx, enabled)` 已恢复官方 08 的布尔构造，并通过 BLE 验证开启/关闭成功响应；该标志没有状态查询，不能声称已读回。RGB 和散热配置独立发送；进程场景入口见下文。

## 程序场景

```sh
./dist/sharkcool scene-monitor --config examples/scenes.json --duration 10s
# 先停止其他设备控制进程，再运行：
./dist/sharkcool scenes --config examples/scenes.json --transport ble
# USB 数据接口可用时，同样使用 --transport usb。
```

编辑示例的 `programs` 为实际可执行文件名或规范的绝对路径。扫描当前用户可读取的 `/proc/PID/exe`，按 JSON 场景顺序匹配，第一个命中生效；没有命中则恢复启动时的设置。`scene-monitor` 仅输出匹配结果，不连接设备。`--poll` 默认为 1 秒，可设置 200ms～60s；`--duration` 到期或 Ctrl+C 后退出，强制杀死进程无法执行恢复。程序参数、环境变量和窗口内容不会被读取。

场景支持六种自主灯效和固定转速配置；`source` 数值 0/1 表示 CPU/GPU，`slot` 为 1～4。曲线可以填写四个 `points`，但必须通过配置槽和延迟生效检查；当前固件仍会回退固定模式，因此该操作报错并恢复，不会报告温控运行。音频和响应灯效目前使用各自独立 CLI，尚未作为场景采集源或网页开关整合。

场景保持期间每五秒重新读回配置，发现设备回退会报错并恢复。当前固件 3.0.3 已通过 BLE 验证固定槽 1：测试程序启动匹配、灯光/1800 RPM 读回、程序退出与 Ctrl+C 后恢复均通过；固定槽 2 的测试出现保存但未生效，不保证所有槽在该固件上可用。

首次运行先读取原灯光、温度来源、生效散热配置及所有可能修改的槽位；备份不一致或失败时不写入。切换会核对完整配置，08 场景标志仅确认成功响应。程序退出后恢复修改的 RGB/散热槽、原生效配置和来源，再明确退出场景模式；原场景标志没有查询接口。恢复失败返回错误。断连期间丢弃旧匹配结果，重连后按最新程序列表重新应用；连接到不同设备时停止，避免写入另一台设备。该入口需要独占设备，不能与 GUI 同时控制。

## 输入响应灯效

```sh
./dist/sharkcool input-monitor --duration 5s  # 只检查桌面活动来源，不连接散热器
# 先退出其他 SharkCool 控制进程，RGB 开关需要开启。
./dist/sharkcool reactive --transport ble --duration 30s
```

响应灯效使用当前 Wayland 会话的 `ext_idle_notifier_v1` v2；通过 `get_input_idle_notification` 观察输入空闲/活动，忽略播放视频等空闲抑制。协议只给出活动状态，不提供键值、字符或鼠标位置，本程序不请求原始键鼠对象、不打开 `/dev/input`、不修改输入权限。协议定义见 [Wayland 官方协议源码](https://github.com/wayland-mirror/wayland-protocols/blob/main/staging/ext-idle-notify/ext-idle-notify-v1.xml)。

先检查桌面支持再改变灯效，活动期间最多每 100 ms 发一次无键值的 16 通知；以最新活动状态为准，设备发送慢或断连时不积压旧活动。没有 `--duration` 时等待 Ctrl+C。停止、超时或采集错误后，尝试恢复完整 RGB 备份并读回，失败会报错。支持取决于桌面提供 v2 协议；已验证本机 niri，X11 和缺少该协议的 Wayland 桌面目前不支持此采集入口。

本机静止观察 3 秒为 0 通知；另一次 40 秒活动观察得到 59 次通知。BLE 响应灯效真机测试 40 秒：20 次活动、20 次完整发送，结束后原刷新灯效八字节读回恢复；另一次 SIGINT 中断也完整恢复了原配置。活动数量不是按键次数。实际 RGB 视觉效果仍需人工核对。网页尚未提供主机灯效开关，目前用独立命令行。

## 播放音频灯效与 RGB 配置查询

```sh
# 先退出其他 SharkCool 控制进程，避免抢占设备；RGB 开关需要开启。
./dist/sharkcool audio --transport ble --duration 30s
# 不指定 duration 时等待 Ctrl+C；结束后恢复启动时备份的 RGB 配置。
./dist/sharkcool rgb-preset --transport ble --effect breathing --palette 0
```

音频采集使用 `parec` 的 `@DEFAULT_MONITOR@` 播放监测接口，需要 PulseAudio 或 PipeWire 的 PulseAudio 兼容服务。采集默认输出设备的播放声音，PCM 不保存；每 100 ms 从双声道 s16le 计算 RMS，并推送 0～3 级强度。该 Linux 强度算法与官方 Windows 频率分析不同，不保证相同音乐下强度一致。停止、持续时间结束或采集错误后，尝试恢复原灯效并读回，失败会返回错误；进程被强制杀死则无法执行恢复。断连时丢弃旧音频，连接恢复后重新设置音频模式并发送新采样。

BLE 真机测试：静音播放流 5 秒采集 49 块，成功发送 49 次，随后原 RGB 配置读回恢复；另一次 SIGINT 测试采集/发送 26 次，同样恢复成功。动态音乐与实际视觉效果仍待核对。网页音频开关尚未接入，目前通过独立命令行使用。

14 查询指定灯效与原始选择值；`--palette` 高半字节的具体名称仍未恢复，不将其标成已确认的配色名称。本机已只读查询全部八种灯效的选择值 0，并确认当前 RGB 设置未改变。响应灯效已通过独立 CLI 接入 Wayland v2 活动通知，见上节；不读取具体键值。

## LCD 布局与系统信息

```sh
./dist/sharkcool lcd --transport ble cpu-temperature fan-rpm time
./dist/sharkcool sysinfo --transport ble
```

`lcd` 需要三个不同指标，按参数顺序指定三行：`cpu-temperature`、`gpu-temperature`、`cpu-usage`、`gpu-usage`、`fan-rpm`、`disk-usage`、`memory-usage`、`time`。对应官方参数布局，设备收到 C2 成功响应后报告 `acknowledged:true`。设备没有已恢复的布局查询接口，因此三行下拉框首次打开和重连时保持未选，属于待应用选择；C1 屏幕开关按设备读回值高亮，每 5 秒刷新。三行布局不能声称已从设备读取。

`sysinfo` 采集一次并发送 07 系统信息。应用的系统信息同步开启后每 5 秒推送，断连后自动继续尝试，退出即停止，每次重新启动默认关闭。系统指标采集在应用层；驱动只接收 `SystemInfo`，不会自行读取主机数据。

温度为整数 °C；CPU 使用率按 `/proc/stat` 的两次采样计算，内存使用率使用 `MemAvailable`，磁盘使用率为根文件系统已用空间。GPU 使用率读取驱动的 `gpu_busy_percent`，或调用本机 `nvidia-smi`；多 GPU 时取可用读数的最大值。时钟为本地当天累计分钟。采集不到的值不发送，零是合法测量值；设备可能保留之前收到的值。`fan-rpm` 由散热器自己提供，不从主机推送。

本机 BLE 已验证真实指标发送、三行 CPU 温度/风扇/时钟布局的成功响应，以及推送开启后的 20 秒观察期间无断连事件。`sent:true` 只表示完整写入传输，系统信息没有读回查询；实际屏幕显示仍需人工核对。

## 更换屏保

在界面的“屏保”区域选择 PNG 或 JPG，预览满意后点击“应用屏保”。支持 8 MB 以内的图片，居中裁剪并缩放至 428×142；预览显示 RGB565 转换后的实际像素。上传前自动读取原图并保存到 `~/.config/sharkcool/screensaver-backups/`，然后逐片上传，最后读回全部图片数据，时间标识、大小、CRC16 和像素完全匹配才报告成功。期间其他设备控制暂时禁用，避免交叉发送命令。

“恢复原图”恢复最近一次更换前保存的图片及原有元数据；备份会保留，程序重启后可继续使用。完整上传与验证需要数分钟，等待页面报告结果再操作。失败会明确显示原因；若已经开始写入，可用“恢复原图”重试恢复。这里更换设备保存的屏保图片，不强制切换 LCD 显示模式，实际进入屏保后的显示仍需核对。

```sh
./dist/sharkcool screen-info --transport usb
./dist/sharkcool screen-backup --transport usb --output original-screen.bin --timeout 120s
```

`screen-info` 查询图片时间标识、大小、CRC16 和保留字节，不代表当前正在显示屏保。命令行备份只有完整校验通过才创建文件，拒绝覆盖现有文件。原图为行优先、大端 RGB565，121,552 字节。USB 已完成原图读取、测试图上传及全像素读回，并恢复原图与原有元数据，全图读回再次匹配；本机 BLE 的 C7 曾返回成功响应，但 120 秒内没有 C8 图片数据，BLE 图片传输尚未通过真机验证。

## 独立驱动 API

应用可以单独导入 `sharkcool-linux/brb02`，不引用网页或桌面模块：

```go
controller, err := brb02.NewWithOptions(brb02.Options{
    Transport: "ble", // 或 "usb" / "auto"
    Address: "",     // 可指定已配对设备的 MAC
}, false)
if err != nil { return err }
go controller.Run(ctx) // ctx 取消后关闭连接；断线自动重连
// 等待 controller.Snapshot().Connected 为 true，再在带时限的 context 下调用：
frame, err := controller.Query(commandCtx, "firmware")
// controller.SetSwitch(commandCtx, "rgb", false)
// controller.SetFanRPM(commandCtx, 1800)
// controller.SetMode(commandCtx, 2)
// controller.GetDeviceOptions(commandCtx)
// controller.UpdateDeviceOptions(commandCtx, brb02.DeviceOptionsPatch{SmartStartStop: &enabled}) // enabled 为 bool
// controller.GetCooling(commandCtx)
// controller.GetCoolingPreset(commandCtx, true, 1)
// controller.SetCoolingSource(commandCtx, brb02.SourceGPU)
// controller.SetCooling(commandCtx, brb02.CoolingConfig{Source: brb02.SourceCPU, Slot: 1, RPM: 1800})
// controller.SetLCDLayout(commandCtx, brb02.LCDLayout{Rows: []brb02.Metric{brb02.CPUTemperature, brb02.FanRPM, brb02.TimeOfDay}})
// controller.SendSystemInfo(commandCtx, brb02.SystemInfo{brb02.CPUTemperature: 35, brb02.TimeOfDay: 608})
// controller.GetRGB(commandCtx)
// controller.GetRGBPreset(commandCtx, 3, 0)
// controller.GetScreenImageInfo(commandCtx)
// controller.GetScreenImage(commandCtx) // 完整图片与 CRC 校验，USB 已验证
// pixels, err := brb02.EncodeScreenImage(sourceImage) // image.Image -> 428×142 RGB565
// controller.SetScreenImage(commandCtx, brb02.ScreenImage{
//     Info: brb02.ScreenImageInfo{Timestamp: timestamp, Size: uint32(len(pixels)), CRC16: brb02.ScreenCRC16(pixels)},
//     Data: pixels,
// }, progress) // 调用前先持久保存 GetScreenImage 返回的原图
// controller.SendAudioLevel(commandCtx, 2)
// controller.SendInputActivity(commandCtx)
// controller.UpdateRGB(commandCtx, brb02.RGBPatch{Effect: "static", Color: "#00ff48"})
```

低层 API：`Discover` / `Open` 对应 USB，`DiscoverBLE` / `OpenBLE` 对应 BLE；`BuildPacket`、`ParseReport` 和 `Decoder` 用于协议分析。`Controller` 提供串行命令、单一读循环、断线清理和恢复；查询返回原始有效帧。BLE 基础控制和 USB 基础查询、RGB 模式切换、屏保读取已实测；其他功能按下表列明验证边界。

可直接运行独立调用示例：`go run ./examples/driver --transport ble`，只导入驱动包，读取固件与 RGB 配置，不启动网页、桌面模块或温度采集。该示例在本机 BLE 上验证通过。

协议、解码、控制器、配置模型和 `Transport` / `Connector` 接口不依赖操作系统。Linux 的 BlueZ、hidraw、自动选择位于 `*_linux.go`，本机传感器采集移至应用层 `internal/sensors/`，播放音频与 Wayland 活动采集位于 `internal/events/`。其他平台可以提供连接器并调用 `NewWithConnector(connector, false)`；连接器在重连时重新打开传输，必须响应 context 取消。Windows x64 已加入本项目的原生 HID / Qt BLE 通信组件和独立桌面打包流程；Windows 真机验证待完成，详见 [Windows 说明](packaging/windows/README-Windows.md)。

已提取的命令和证据见 [PROTOCOL.md](PROTOCOL.md)。自定义散热曲线完整启用、主机事件型灯效的网页入口仍在移植中。此版本不开放固件升级、引导或恢复出厂命令。

| 官方功能 | 当前状态 |
| --- | --- |
| 固件、心跳、固定转速、配置读回 | BLE 真机通过 |
| RGB/LCD 开关 | BLE 写入与读回通过 |
| 六种自主 RGB 灯效、亮度、颜色、速度参数 | BLE 配置读回通过；USB 单色/彩色切换读回通过；视觉核对待做 |
| USB 通信、USB 热插拔 | 原配 C-to-C 接 USB-IN 后，真实心跳与基础查询通过；热插拔恢复有软件测试 |
| 智能启停、上电自启动选项 | BLE 读写与保留未修改字段通过；断电启动行为待验证 |
| 固定/四点曲线配置槽读写、CPU/GPU 温度来源 | BLE 全部八槽读取、曲线保存与来源切换通过 |
| 温控曲线运行、设备档位切换 | 完整启用流程仍在恢复，保存成功与当前生效分开报告 |
| LCD 三行参数布局 | BLE 成功响应通过；实际显示待人工核对 |
| 系统信息推送 | Linux 采集、单次/持续推送已实现；BLE 写入通过，屏幕显示待核对 |
| 八种 RGB 指定配置查询 | BLE 选择值 0 的全部八种读取通过；读取未改变当前设置 |
| 音频灯效 | Linux 命令行采集/推送、退出恢复已实现；BLE 静音流通过，动态音乐视觉效果待核对 |
| 响应灯效、进程场景联动 | Wayland v2 活动采集、CLI 响应推送及恢复 BLE 验证通过；程序匹配场景 CLI 已实现；视觉核对和网页入口待补齐 |
| 屏保图片传输 | USB 原图备份、测试图上传、原图恢复和整图读回通过；网页支持预览、上传和原图恢复；BLE 图片传输待验证 |
| 风扇独立开关 | 帧构造已实现，真机状态变化未验证 |

## 安装与权限

解压对应架构的桌面应用包后，在包目录运行：

```sh
sh scripts/install.sh --desktop
```

安装到 `~/.local/bin/sharkcool`，创建应用菜单中的“SharkCool · 黑鲨风神Pro”、图标和桌面上的 `SharkCool.desktop`。随后直接从菜单或桌面启动，无需手动打开终端；已运行时再次启动只打开已有服务的控制窗口。启动器使用安装后的绝对路径，移动或删除解压目录不会影响已安装的应用。更新时重复运行安装命令，会原子替换程序，已运行的实例在下次重新启动后使用新版；更新后需要重启后台驱动与原生窗口，设置页显示运行中的驱动版本。

不需要桌面快捷方式时运行 `sh scripts/install.sh`；自定义目录用 `--prefix /absolute/path`。桌面环境若未开启桌面图标，请从应用菜单启动，文件管理器也可以打开 `SharkCool.desktop`（部分环境首次需要选择“允许启动”）。默认打开 Qt 原生应用 `sharkcool-window`，拥有独立进程、图标和 Wayland 应用 ID `sharkcool`，没有浏览器依赖。再次点击启动器激活已有原生窗口。网页入口保留为可选的 `sharkcool gui --browser`；不会在原生窗口缺失时静默打开浏览器。登录自启在应用“设置”中单独开启，不随安装自动启用。

BLE 使用系统 BlueZ 和当前用户的 D-Bus 权限，无需 USB udev 规则。USB 初次使用可安装仅匹配 BRB02 的规则：

```sh
sudo install -m 644 packaging/70-sharkcool.rules /etc/udev/rules.d/70-sharkcool.rules
sudo udevadm control --reload-rules
sudo udevadm trigger --subsystem-match=hidraw
```

重新插拔实际 USB 数据连接后检查 `doctor`；应用本身不需要 root。仅供电连接不会出现可操作的 hidraw 设备。

## 构建与验证

后台依赖 Go 1.22+；原生前端依赖 CMake、C++17 编译器、Qt 6.4+ Widgets / Network / Svg。前端使用系统 Qt，不包含 WebView 或 Chromium。当前 amd64 预编译桌面包用 Qt 6.12 构建，运行时需要相应或更新的 Qt 与兼容的系统库；其他发行版建议在本机编译前端。arm64 包当前只含后台驱动与命令行，原生界面需在 arm64 本机编译。唯一第三方 Go 模块 `github.com/godbus/dbus/v5` 已保存在 `vendor/`，可离线构建；后台发布二进制为 `CGO_ENABLED=0`。BLE 运行时需要系统 BlueZ 和系统 D-Bus。

```sh
make build    # dist/sharkcool + dist/sharkcool-window
make driver   # 只构建后台，无 Qt 编译依赖
make test     # Go race、网页与 Qt 原生界面测试；需要 Qt Test 与 dbus-daemon
make check    # go vet、Node.js 语法检查、安装脚本语法检查
make release  # amd64 桌面包 / arm64 命令行包 / 源码包
```

BLE 集成测试启动隔离的 D-Bus 测试服务，验证通知接收、断连检测、AE10 写入及剥离 USB 填充；不连接硬件。受限沙箱可能禁止本地套接字，需要在普通宿主机环境运行测试。协议测试覆盖真机心跳、固件响应、官方发送格式、发送校验与接收差异、分片/多帧解码，以及 USB FD 读写、取消、自动重连和 HTTP 控制边界。

发布还包含 `dist/sharkcool-source.tar.gz`，仅包含独立源码、vendor 依赖、示例和构建/安装文件，安装上述编译依赖后可使用 `GOPROXY=off make build` 离线构建。不包含官方 EXE/DLL、分析文件或上游检出。核心跨平台编译检查：`GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go test -c -o /tmp/brb02.test.exe ./brb02`。

## 排查蓝牙断连

`doctor` 分别显示已配对、已连接、服务已解析、控制接口可用四种状态。此次通过 `linklog` 捕获到 7 次 `org.bluez.Reason.Authentication`，随后清除这台设备旧记录并重新配对，服务解析和 BLE 控制恢复；30 秒独立观察中没有再收到断连信号，并已验证主动断开后自动重连、恢复有效心跳。该结果只说明本次验证期间稳定，不保证长期或断电后不会再出现问题。

同类故障先查看 `linklog` 的断连原因。HID / PNP 读取错误本身不能解释原因。若明确为认证失败，可仅针对 BRB02 重建配对；不要直接修改所有设备的系统 HID 配置。

若 D-Bus 日志不足以定位问题，可以在保持散热器通电时抓取只读 HCI 日志（需要管理员权限）：

```sh
mkdir -p analysis
sudo timeout 20s btmon -w "$PWD/analysis/brb02-link.btsnoop"
btmon -r analysis/brb02-link.btsnoop
```

抓包可能包含其他蓝牙设备信息，应仅在本地分析。`analysis/` 和官方提取目录 `reference/` 均不包含在发布包中。

## 来源与许可

上游 [H-table/sharkcool](https://github.com/H-table/sharkcool)，参考提交 `ec2141e483c8a962e05edbd44743c68e1cd9a214`；官方安装包为用户提供。上游对帧头/校验字节的解释已根据官方 DLL 修正。保留 MIT 许可 [LICENSE](LICENSE)。第三方 D-Bus 模块的 BSD 许可保存在 `packaging/licenses/godbus-dbus.txt`，并随二进制发布。官方 Windows 二进制仅作为本地分析参考，不随 Linux 驱动分发。

快捷档位从设备读取当前控制方式对应的四个配置槽，点击立即应用，写入前重新读取所选槽，不覆盖该槽保存的转速或曲线。高亮和“当前”信息以 25 查询的实际配置为准，每 5 秒更新；手动修改转速或曲线仍需应用。USB 固件 3.0.3 已验证 2/3/4 号槽持续生效并恢复原槽。散热器屏显档位仍需目视核对，心跳 field2 不作为档位解释。

原生界面覆盖实时 RPM、CPU/GPU 温度走势、真实四档即时切换、固定转速与四点曲线、六种自主 RGB 灯效与单色/彩色、LCD 开关/三行布局设置、屏保预览/上传/原图恢复、启动选项、系统信息同步和登录自启。界面关闭后后台驱动继续运行。Qt 测试使用隔离的本地模拟服务，覆盖设备读回、配置草稿保护、断连重连、屏保独占和令牌刷新，不访问散热器。

1.11.1 修复编辑档位时沿用上一档数值的问题：选择编辑档位自动载入该槽配置，固定转速应用前检查相邻档位，要求一至四档严格递增。设备初始固定转速的真实读回为 1200 / 2134 / 2912 / 3534 RPM；界面始终显示实际设备槽值，不在启动时自动覆盖。

1.11.2 增加已验证的 `query fan-switch`（21）读回，并拒绝把非零开关响应当作命令接受。当前 USB 固件 3.0.3 可以独立关闭灯光和屏幕并保持连接，但 20/00 关闭风扇不生效，整机开关 27 返回拒绝码 01；开启智能启停仍相同。完整“一键关闭风扇、灯光、屏幕且保留连接”的休眠功能尚未实现。真实验证已恢复所有开关与原档位。

四个档位是设备自身的配置槽，当前界面的自定义转速/曲线会编辑这些槽，并非额外新增的本机方案。固定配置和曲线各有四槽；软件可以额外保存更多本机方案，但当前界面尚无独立方案库，启用它们仍需使用设备现有槽位。

1.12.1 修复灯效开关接口遗漏 `enabled` 字段的问题，新增设备协议响应到真实 HTTP 接口的回归测试；概览与灯效页由 11 命令读回实际开关状态。Windows 构建流程校验编译、自动测试、完整安装包的启动与卸载；散热器真机验证仍待完成。
