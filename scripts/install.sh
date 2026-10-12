#!/bin/sh
set -eu
task_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
task_prefix=${PREFIX:-"$HOME/.local"}
task_desktop=false
while [ "$#" -gt 0 ]; do
  case "$1" in
    --prefix) [ "$#" -ge 2 ] || { echo "--prefix 缺少路径" >&2; exit 1; }; task_prefix=$2; shift 2 ;;
    --desktop) task_desktop=true; shift ;;
    --help|-h) echo "用法: $0 [--prefix /absolute/path] [--desktop]"; exit 0 ;;
    *) echo "用法: $0 [--prefix /absolute/path] [--desktop]" >&2; exit 1 ;;
  esac
done
case "$task_prefix" in /*) ;; *) echo "安装前缀必须为绝对路径" >&2; exit 1;; esac
case "$task_prefix" in *'='*) echo "桌面启动器不支持安装路径中的等号" >&2; exit 1;; esac
case "$task_prefix" in *'
'*) echo "安装路径不能包含换行" >&2; exit 1;; esac
task_binary="$task_root/sharkcool"
if [ ! -f "$task_binary" ]; then task_binary="$task_root/dist/sharkcool"; fi
if [ ! -f "$task_binary" ]; then echo "未找到可执行文件，请先运行 make build" >&2; exit 1; fi
task_window="$task_root/sharkcool-window"
if [ ! -f "$task_window" ]; then task_window="$task_root/dist/sharkcool-window"; fi
task_native=false
if [ -f "$task_window" ]; then
  "$task_window" --version >/dev/null || { echo "Qt 原生应用运行库未就绪；请查看 README 或在本机重新构建前端" >&2; exit 1; }
  task_native=true
elif [ "$task_desktop" = true ]; then
  echo "当前包不含原生窗口，请安装 amd64 桌面包，或先运行 make desktop" >&2
  exit 1
fi
# Atomic replacement also works when the installed executable is running.
mkdir -p "$task_prefix/bin"
task_temp=$(mktemp "$task_prefix/bin/.sharkcool-XXXXXX")
trap 'rm -f "$task_temp"' 0
install -m755 "$task_binary" "$task_temp"
mv -f "$task_temp" "$task_prefix/bin/sharkcool"
if [ "$task_native" = true ]; then
  task_temp=$(mktemp "$task_prefix/bin/.sharkcool-window-XXXXXX")
  install -m755 "$task_window" "$task_temp"
  mv -f "$task_temp" "$task_prefix/bin/sharkcool-window"
fi
if [ "$task_native" = true ]; then
install -Dm644 "$task_root/packaging/sharkcool.svg" "$task_prefix/share/icons/hicolor/scalable/apps/sharkcool.svg"
# Use an absolute Exec path so a desktop session needn't have ~/.local/bin in PATH.
# A quoted Exec has both desktop-value and shell-like argument escaping.
task_exec=$(printf '%s' "$task_prefix/bin/sharkcool" | sed 's/%/%%/g; s/\\/\\\\/g; s/["`$]/\\&/g; s/\\/\\\\/g')
mkdir -p "$task_prefix/share/applications"
while IFS= read -r task_line; do
  case "$task_line" in
    Exec=*) printf 'Exec="%s" gui\n' "$task_exec" ;;
    *) printf '%s\n' "$task_line" ;;
  esac
done < "$task_root/packaging/sharkcool.desktop" > "$task_prefix/share/applications/sharkcool.desktop"
if command -v desktop-file-validate >/dev/null 2>&1; then
  desktop-file-validate "$task_prefix/share/applications/sharkcool.desktop"
fi
if command -v update-desktop-database >/dev/null 2>&1; then
  update-desktop-database "$task_prefix/share/applications" || :
fi
if command -v gtk-update-icon-cache >/dev/null 2>&1; then
  gtk-update-icon-cache -f -t "$task_prefix/share/icons/hicolor" >/dev/null 2>&1 || :
fi
if [ "$task_desktop" = true ]; then
  if command -v xdg-user-dir >/dev/null 2>&1; then
    task_desktop_dir=$(xdg-user-dir DESKTOP)
  else
    task_desktop_dir="$HOME/Desktop"
  fi
  if [ -n "$task_desktop_dir" ] && [ "$task_desktop_dir" != "$HOME" ]; then
    mkdir -p "$task_desktop_dir"
    install -m755 "$task_prefix/share/applications/sharkcool.desktop" "$task_desktop_dir/SharkCool.desktop"
    if command -v gio >/dev/null 2>&1; then
      gio set -t boolean "$task_desktop_dir/SharkCool.desktop" metadata::trusted true >/dev/null 2>&1 || :
    fi
    echo "桌面快捷方式：$task_desktop_dir/SharkCool.desktop"
  else
    echo "桌面目录已禁用，应用菜单入口已安装。"
  fi
fi
fi
echo "已安装：$task_prefix/bin/sharkcool"
if [ "$task_native" = true ]; then
  echo "原生应用：$task_prefix/bin/sharkcool-window"
else
  echo "此包仅包含后台驱动与命令行；原生窗口需在本机运行 make desktop 后安装。"
fi
if [ "$task_native" = true ]; then
  echo "USB 权限规则请按 README 中的命令安装；随后可从应用菜单打开 SharkCool。"
else
  echo "后台使用 gui --no-browser；可选网页使用 gui --browser。USB 权限规则见 README。"
fi
