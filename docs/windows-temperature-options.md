# Windows 温度采集

安装版自动准备温度采集组件，便携版在首次启动时自动初始化。应用通过本地服务获取 CPU / GPU 温度和 GPU 使用率，无需另开监控软件。

## 采集流程

`Go 后台 → 本地命名管道 → SharkCoolSensors 服务 → LibreHardwareMonitor / PawnIO`

- CPU 优先选择封装或 Die 温度，GPU 优先选择核心温度。
- 服务统一采集 CPU / GPU，短时间内的重复请求共用采样，退出应用后停止轮询。
- Go 直接读取管道，服务使用普通用户只读访问权限；读取前核对服务进程身份，并设置超时与响应大小上限。
- NVIDIA 保留 `nvidia-smi` 作为后备来源。

## 组件

- [LibreHardwareMonitor](https://github.com/LibreHardwareMonitor/LibreHardwareMonitor)
- [PawnIO](https://pawnio.eu/)
