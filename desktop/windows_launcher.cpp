#define NOMINMAX
#include <windows.h>
#include <string>
int WINAPI wWinMain(HINSTANCE,HINSTANCE,PWSTR arguments,int){
 wchar_t filename[32768];DWORD count=GetModuleFileNameW(nullptr,filename,32768);if(count==0||count>=32768)return 1;
 std::wstring folder(filename,count);folder.resize(folder.find_last_of(L"\\/"));std::wstring executable=folder+L"\\sharkcool-service.exe";
 std::wstring command=L"\""+executable+L"\" "+(arguments&&*arguments?arguments:L"gui");STARTUPINFOW startup{};startup.cb=sizeof(startup);PROCESS_INFORMATION process{};
 if(!CreateProcessW(executable.c_str(),command.data(),nullptr,nullptr,FALSE,CREATE_NO_WINDOW,nullptr,folder.c_str(),&startup,&process)){MessageBoxW(nullptr,L"无法启动 SharkCool 服务。请完整解压应用包，并保留所有程序和 DLL 文件。",L"SharkCool",MB_OK|MB_ICONERROR);return 1;}
 CloseHandle(process.hThread);CloseHandle(process.hProcess);return 0;
}
