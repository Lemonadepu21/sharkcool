# Third-party components

This portable distribution dynamically links Qt Core, Gui, Widgets, Network, Svg and Bluetooth (Qt 6.8.3). The Qt runtime libraries remain replaceable files. Qt is licensed under LGPL-3.0 or another applicable Qt license; texts are included here and in the original Qt package's LICENSES directory when present. Qt sources for the corresponding release are available at https://download.qt.io/archive/qt/6.8/6.8.3/single/ and https://code.qt.io/ . The application source includes its own build and packaging scripts. No static Qt linking is used.

Microsoft Visual C++ CRT runtime DLLs are deployed app-locally from the Visual Studio 2022 Redistributable directory. Their applicable Microsoft redistribution terms apply. Windows 10 supplies the Universal CRT. No separate runtime installer is needed.

The application icon depicts Shark Chan and remains the property of its original rights holders. See ASSET-NOTICE.md; the source project's MIT license does not grant rights in this artwork. This application is not an official Black Shark product.

The Linux-specific godbus library is not linked into the Windows service. Its license is retained with the shared source distribution.

CPU temperature monitoring uses LibreHardwareMonitorLib 0.9.6 (MPL-2.0), dynamically included in the self-contained .NET 10 sensor executable. Corresponding unmodified source is available at https://github.com/LibreHardwareMonitor/LibreHardwareMonitor/tree/v0.9.6 . Its MPL license and third-party notices accompany this distribution. The sensor helper's own source is in desktop/sensors/.

The unmodified, digitally signed PawnIO installer is redistributed from LibreHardwareMonitor v0.9.6, SHA-256 a3a46226c5e2824f4cdd42be0eecbabfc672c86f7889710f5ab1e6ad385b47a0. Official information and source: https://pawnio.eu/ and https://github.com/namazso/PawnIO . The GPL license is included. Installation is limited to the signed official edition; unrestricted driver mode is never selected. PawnIO is shared with other monitoring applications and is not removed automatically.

The sensor helper carries the .NET 10 runtime and Microsoft service/pipe libraries; no system-wide .NET runtime installation is required. .NET sources and notices: https://github.com/dotnet/runtime .
