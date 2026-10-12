# SharkCool Windows 预览版

适用于 Windows 10/11 x64。此版包含实际 Windows HID 与 BLE 后端、独立 Qt 控件窗口，未使用浏览器、WebView、Wine 或官方驱动二进制。

推荐下载 `SharkCool-1.12.1-windows-x64-setup.exe`，双击安装即会创建桌面与开始菜单入口，包含 Qt 和 C++ 运行库，无需管理员权限。安装时不会自动开启登录自启。

若选便携 ZIP：

1. 将整个 ZIP 解压到固定目录；保留三个组件、Qt DLL 和 plugins 子目录。
2. 双击 `SharkCool.exe`。USB 请使用 USB-IN 数据接口；蓝牙请先在 Windows 设置配对 `BS BRB02 Cooler Pro`，再启动应用。
3. 双击 `Install-Shortcuts.cmd` 创建当前用户桌面与开始菜单快捷方式；无需管理员权限。安装后不要移动应用目录。

概览页显示真实转速，屏幕、灯效可独立切换。固定转速、四档配置、温控曲线编辑、RGB、屏保上传/恢复和设备启动设置沿用 Linux 版。首次连接与重新连接只读取设备状态，不自动覆盖四档转速；命令失败不自动重放。

关闭窗口后后台继续运行；再次打开只激活同一实例。需要结束后台时运行 `Stop-SharkCool.cmd`。登录自启在应用「设置」单独开启；解压与创建快捷方式不会自动开启。

本版是 Windows 预览版。CI 检查 Windows 编译、自动测试、打包后运行库、演示服务和真实 Qt 窗口；这些检查不等同于 Windows 散热器真机验证。USB 拔插恢复、蓝牙掉线恢复、屏保传输与实际视觉状态仍需 Windows 真机验证。

Windows 系统信息可读取 CPU 使用率、内存、系统盘、时钟；NVIDIA 安装 `nvidia-smi` 时显示 GPU 温度/利用率。CPU 温度和其他显卡温度暂不支持，界面显示「—」，对应温控曲线请勿依赖未提供的温度来源。主机音频/输入采集和只读 BLE 链路日志暂不支持；设备内置 RGB 配置和常用控制仍可用。完整关闭风扇尚未实现；屏幕/灯效关闭保持连接。

官方控制软件若同时访问设备可能争抢响应，请关闭后再用本应用。默认监听仅 `127.0.0.1:17865`，不开放局域网。日志位于 `%LOCALAPPDATA%\sharkcool\service.log` 和 `window.log`；备份在用户缓存目录，未写入应用安装目录。

命令行可用 `sharkcool-service.exe doctor`、`version`、`gui --transport usb`、`gui --transport ble --address AA:BB:CC:DD:EE:FF`。GUI 子系统程序在脚本中应使用 `Start-Process -Wait` 或重定向输出。全部 CLI 参数见项目 README。
