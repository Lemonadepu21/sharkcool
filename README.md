# SharkCool

黑鲨风神 Pro（BRB02）的独立控制应用，支持 **Linux 和 Windows**。

原生桌面界面，通过 USB 或蓝牙控制散热器，查看转速与主机温度，调整灯效、屏幕和散热配置。

[下载最新版本](https://github.com/Lemonadepu21/sharkcool/releases/latest) · [反馈问题](https://github.com/Lemonadepu21/sharkcool/issues) · [MIT 许可证](LICENSE)

## 功能

- **风扇控制**：实时转速、四档一键切换、自定义固定转速、四点温控曲线配置。
- **RGB 灯效**：六种内置灯效，单色 / 彩色、亮度与速度调节，自动读取当前设置。
- **屏幕控制**：独立关闭屏幕和灯光，设置三行显示指标，推送主机系统信息。
- **自定义屏保**：图片预览、USB 上传、原图备份与恢复。
- **主机监控**：CPU / GPU 温度、使用率、内存与磁盘信息。
- **桌面集成**：独立窗口与图标、系统托盘、登录自启动；关闭窗口后继续驻留。
- **连接恢复**：USB / BLE 自动连接与断线重试，重连后读取设备当前状态。

四档对应设备原有的配置槽；固定转速和曲线各有四槽。选择档位直接生效，编辑配置后再应用。

## 下载

在 [Releases](https://github.com/Lemonadepu21/sharkcool/releases/latest) 下载对应文件：

| 系统 | 文件 | 说明 |
| --- | --- | --- |
| Windows 10 / 11 x64 | `SharkCool-1.14.0-windows-x64-setup.exe` | 推荐，安装后从桌面或开始菜单启动 |
| Windows 10 / 11 x64 | `SharkCool-1.14.0-windows-x64.zip` | 便携版，完整解压后运行 `SharkCool.exe` |
| Linux x86_64 | `sharkcool-linux-amd64.tar.gz` | 原生桌面应用与命令行 |
| Linux ARM64 | `sharkcool-linux-arm64.tar.gz` | 仅后台与命令行，桌面界面需自行编译 |
| 源码 | `sharkcool-source.tar.gz` | 含 vendor 依赖、构建脚本与示例 |

发行文件附带 SHA-256 校验值。Linux 桌面包在 Ubuntu 24.04 构建，使用系统 Qt 6。

## 安装与连接

### Windows

1. 双击安装 EXE，按向导完成安装。
2. 使用 USB-IN 数据接口连接电脑，或先在系统蓝牙设置中配对 `BS BRB02 Cooler Pro`。
3. 从桌面或开始菜单打开 SharkCool。

安装包自动准备 Qt、C++、.NET 运行库和 CPU/GPU 采集服务，并安装官方签名 PawnIO 驱动。安装需要一次管理员授权；完成后直接启动应用，无需单独配置采集组件或另开监控软件，日常运行和登录自启使用普通权限。

便携版需完整解压，双击 `SharkCool.exe`。首次打开会自动初始化或更新温度采集组件，按系统提示授权即可；再次打开会复用已有服务，登录自启不会弹出提权提示。取消初始化后仍可控制散热器。可用 `Install-Shortcuts.cmd` 创建快捷方式。更多说明见 [Windows 使用说明](packaging/windows/README-Windows.md)。

### Linux

桌面运行需要 Qt 6 Widgets、Network、Svg；蓝牙连接需要 BlueZ 与系统 D-Bus。

Ubuntu / Debian 可安装运行依赖：

```sh
sudo apt install libqt6widgets6 libqt6network6 libqt6svg6 bluez
```

Arch Linux：

```sh
sudo pacman -S qt6-base qt6-svg bluez bluez-utils
```

解压下载的包，在包目录执行：

```sh
sh scripts/install.sh --desktop
```

安装后从应用菜单或桌面打开 SharkCool。更新时重复安装，完全退出应用后重新打开即可使用新版。

USB 首次使用需安装设备权限规则，随后重新插拔数据线；应用本身无需 root：

```sh
sudo install -m 644 packaging/70-sharkcool.rules /etc/udev/rules.d/70-sharkcool.rules
sudo udevadm control --reload-rules
sudo udevadm trigger --subsystem-match=hidraw
```

蓝牙请先在系统中配对 `BS BRB02 Cooler Pro`，无需 USB 权限规则。

### 日常使用

- **连接**：使用支持数据传输的线连接散热器的 **USB-IN**；独立电源口可能仅供电。蓝牙与 USB 均可使用。
- **托盘**：关闭窗口后继续运行；托盘菜单可重新打开窗口、设置自启或完全退出。登录自启时若桌面没有可用托盘，则直接显示窗口。
- **登录自启**：在应用「设置」或托盘菜单中开启，登录后驻留托盘；安装不会默认开启自启。
- **屏保**：选择图片并预览，上传前自动备份原图；传输可能需要数分钟，请等待完成。失败后可重新连接并恢复原图。
- **屏保显示**：上传完成后，约 10 秒不操作设备会自动进入屏保。使用机身按键更改设置后，点击「刷新设备」读取当前配置。
- **避免冲突**：关闭官方控制软件及其他占用散热器的程序，再使用 SharkCool。

## 命令行与开发

安装后可用 `sharkcool`；Windows 命令行入口为 `sharkcool-service.exe`。完整命令与选项见 `sharkcool help`。

```sh
sharkcool doctor                         # 检查设备与连接权限
sharkcool gui --transport usb            # 使用 USB 打开应用
sharkcool gui --transport ble            # 使用蓝牙打开应用
sharkcool status --transport usb         # 查询状态
sharkcool switch --transport usb lcd off # 关闭屏幕
sharkcool gui --demo                     # 无散热器演示界面
```

默认本地控制服务仅监听 `127.0.0.1:17865`。原生窗口使用 Qt Widgets；可选网页入口用 `sharkcool gui --browser` 显式打开。

独立 Go 驱动包位于 [`brb02/`](brb02)，可供其他应用调用。参见 [调用示例](examples/driver) 和 [协议说明](PROTOCOL.md)。

Linux 构建需要 Go 1.22+、CMake、C++17 编译器和 Qt 6.4+ Widgets / Network / Svg；运行测试还需要 Qt Test、Node.js 与 `dbus-daemon`。Go 依赖已存入 `vendor/`。

```sh
make build    # 原生桌面应用与后台
make driver   # 仅后台与命令行
make test     # 驱动、界面及连接恢复测试
make check    # 静态检查
make release  # Linux 桌面包、ARM64 命令行包和源码包
```

Windows 构建需要 VS 2022 C++ 工具、Go、CMake、Python、Node.js、.NET 10 SDK、Inno Setup 6 和 Qt 6.8.3（Widgets / Network / Svg / Bluetooth / Test）：

```powershell
./scripts/build-windows.ps1 -QtRoot C:/Qt/6.8.3/msvc2022_64
```

## 许可与致谢

项目源码使用 **[MIT License](LICENSE)**，允许使用、修改和再分发，需保留版权与许可声明。

第三方组件遵循各自许可证，见 [Windows 第三方组件说明](packaging/windows/THIRD-PARTY.md) 和 [`packaging/licenses/`](packaging/licenses)。鲨鲨酱图标及黑鲨标识不适用源码的 MIT 许可，权利属于原权利人，见 [素材说明](packaging/ASSET-NOTICE.md)。

感谢 [H-table/sharkcool](https://github.com/H-table/sharkcool) 提供的早期协议研究。本项目为非官方设备控制工具，与黑鲨官方无隶属关系。
