# Third-party components

This portable distribution dynamically links Qt Core, Gui, Widgets, Network, Svg and Bluetooth (Qt 6.8.3). The Qt runtime libraries remain replaceable files. Qt is licensed under LGPL-3.0 or another applicable Qt license; texts are included here and in the original Qt package's LICENSES directory when present. Qt sources for the corresponding release are available at https://download.qt.io/archive/qt/6.8/6.8.3/single/ and https://code.qt.io/ . The application source includes its own build and packaging scripts. No static Qt linking is used.

Microsoft Visual C++ CRT runtime DLLs are deployed app-locally from the Visual Studio 2022 Redistributable directory. Their applicable Microsoft redistribution terms apply. Windows 10 supplies the Universal CRT. No separate runtime installer is needed.

The application icon depicts Shark Chan and remains the property of its original rights holders. See ASSET-NOTICE.md; the source project's MIT license does not grant rights in this artwork. This application is not an official Black Shark product.

The Linux-specific godbus library is not linked into the Windows service. Its license is retained with the shared source distribution.
