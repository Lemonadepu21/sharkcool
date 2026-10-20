using LibreHardwareMonitor.Hardware;
using LibreHardwareMonitor.PawnIo;
using Microsoft.Win32.SafeHandles;
using System.IO.Pipes;
using System.Runtime.InteropServices;
using System.Security.AccessControl;
using System.Security.Principal;
using System.ServiceProcess;
using System.Text;
using System.Text.Json;

internal static class Program
{
    internal const string Name = "SharkCoolSensors";
    internal const string Pipe = "SharkCool-CPU-Temperature";

    internal static int? SelectCPU(IEnumerable<(string Name, float? Value)> sensors)
    {
        var valid = sensors.Where(s => !s.Name.Contains("Distance to TjMax", StringComparison.OrdinalIgnoreCase) && s.Value is float v && float.IsFinite(v) && v > 0 && v <= 150).ToArray();
        var package = valid.Where(s => s.Name == "CPU Package" || s.Name.Contains("Tctl/Tdie", StringComparison.OrdinalIgnoreCase)).ToArray();
        var selected = package.Length > 0 ? package : valid;
        return selected.Length == 0 ? null : (int)Math.Round(selected.Max(s => s.Value!.Value), MidpointRounding.AwayFromZero);
    }

    internal static int? SelectGPU(IEnumerable<(string Name, float? Value)> sensors, bool load = false)
    {
        var valid = sensors.Where(s => s.Value is float v && float.IsFinite(v) && v >= (load ? 0 : 1) && v <= (load ? 100 : 150)).ToArray();
        var core = valid.Where(s => s.Name.Equals("GPU Core", StringComparison.OrdinalIgnoreCase)).ToArray();
        // Never substitute VRAM/hotspot temperature or memory utilization for GPU core readings.
        var selected = core.Length > 0 ? core : valid.Where(s => load ? s.Name.StartsWith("D3D 3D", StringComparison.OrdinalIgnoreCase) : s.Name.Equals("GPU", StringComparison.OrdinalIgnoreCase)).ToArray();
        return selected.Length == 0 ? null : (int)Math.Round(selected.Max(s => s.Value!.Value), MidpointRounding.AwayFromZero);
    }

    private static async Task<int> Main(string[] args)
    {
        try
        {
            if (args.SequenceEqual(new[] { "--self-test" }))
            {
                if (SelectCPU(new[] { ("CPU Package", (float?)55.5f), ("CPU Core #1", (float?)80) }) != 56 ||
                    SelectCPU(new[] { ("CPU Core #1", (float?)45), ("CPU Core #2", (float?)50), ("CPU Core #1 Distance to TjMax", (float?)90) }) != 50 ||
                    SelectCPU(new[] { ("CPU Core #1 Distance to TjMax", (float?)90) }) != null ||
                    SelectCPU(new[] { ("Core", (float?)float.NaN), ("Core", (float?)151), ("Core", (float?)null) }) != null)
                    throw new InvalidOperationException("CPU sensor selection failed");
                if (SelectGPU(new[] { ("GPU Core", (float?)61), ("GPU Hot Spot", (float?)95), ("GPU Memory", (float?)90) }) != 61 ||
                    SelectGPU(new[] { ("GPU Memory", (float?)90) }) != null ||
                    SelectGPU(new[] { ("GPU Core", (float?)0), ("GPU Memory Controller", (float?)99) }, true) != 0 ||
                    SelectGPU(new[] { ("D3D 3D", (float?)42), ("D3D Copy", (float?)90) }, true) != 42 ||
                    SelectGPU(new[] { ("GPU Core", (float?)float.NaN), ("GPU", (float?)151) }) != null)
                    throw new InvalidOperationException("GPU sensor selection failed");
                Console.WriteLine("CPU/GPU sensor selection passed");
                return 0;
            }
            if (args.SequenceEqual(new[] { "--read" }))
            {
                using var pipe = new NamedPipeClientStream(".", Pipe, PipeDirection.In, PipeOptions.Asynchronous);
                await pipe.ConnectAsync(800);
                if (!GetNamedPipeServerProcessId(pipe.SafePipeHandle, out uint pid) || pid != ServicePID())
                    throw new IOException("Temperature pipe does not belong to SharkCoolSensors");
                using var reader = new StreamReader(pipe, Encoding.UTF8);
                string? line = await reader.ReadLineAsync().WaitAsync(TimeSpan.FromSeconds(3));
                if (line == null || line.Length > 1024) throw new IOException("Invalid CPU temperature response");
                Console.WriteLine(line);
                return 0;
            }
            if (args.Length != 0) return 2;
            ServiceBase.Run(new TemperatureService());
            return 0;
        }
        catch (Exception e)
        {
            Console.Error.WriteLine(e.Message);
            return 1;
        }
    }

    private sealed class TemperatureService : ServiceBase
    {
        private CancellationTokenSource? stop;
        private Task? worker;
        public TemperatureService() { ServiceName = Name; CanStop = true; AutoLog = false; }
        protected override void OnStart(string[] args)
        {
            stop = new CancellationTokenSource();
            worker = Task.Run(async () =>
            {
                try { await Serve(stop.Token); }
                catch (Exception e)
                {
                    Console.Error.WriteLine(e);
                    Environment.Exit(1);
                }
            });
        }
        protected override void OnStop()
        {
            stop?.Cancel();
            worker?.Wait(TimeSpan.FromSeconds(5));
            stop?.Dispose();
        }
        private static async Task Serve(CancellationToken stop)
        {
            var computer = new Computer { IsCpuEnabled = true, IsGpuEnabled = true };
            bool opened = false;
            string? cached = null;
            long sampled = 0;
            var security = new PipeSecurity();
            security.AddAccessRule(new PipeAccessRule(new SecurityIdentifier(WellKnownSidType.NetworkSid, null), PipeAccessRights.FullControl, AccessControlType.Deny));
            security.AddAccessRule(new PipeAccessRule(new SecurityIdentifier(WellKnownSidType.LocalSystemSid, null), PipeAccessRights.FullControl, AccessControlType.Allow));
            security.AddAccessRule(new PipeAccessRule(new SecurityIdentifier(WellKnownSidType.BuiltinAdministratorsSid, null), PipeAccessRights.FullControl, AccessControlType.Allow));
            security.AddAccessRule(new PipeAccessRule(new SecurityIdentifier(WellKnownSidType.AuthenticatedUserSid, null), PipeAccessRights.Read | PipeAccessRights.Synchronize, AccessControlType.Allow));
            try
            {
                while (!stop.IsCancellationRequested)
                {
                    using var pipe = NamedPipeServerStreamAcl.Create(Pipe, PipeDirection.Out, 1, PipeTransmissionMode.Byte, PipeOptions.Asynchronous, 0, 1024, security);
                    await pipe.WaitForConnectionAsync(stop);
                    if (cached == null || Environment.TickCount64 - sampled >= 2000)
                    {
                        int? cpu = null, gpu = null, gpuUsage = null;
                        string? error = null;
                        try
                        {
                            if (!opened) { computer.Open(); opened = true; }
                            var cpuSensors = new List<(string, float?)>();
                            var gpuTemperatures = new List<int>();
                            var gpuLoads = new List<int>();
                            foreach (var hardware in computer.Hardware)
                            {
                                try
                                {
                                    hardware.Update();
                                    if (hardware.HardwareType == HardwareType.Cpu)
                                        cpuSensors.AddRange(hardware.Sensors.Where(s => s.SensorType == SensorType.Temperature).Select(s => (s.Name, s.Value)));
                                    else if (hardware.HardwareType is HardwareType.GpuAmd or HardwareType.GpuNvidia or HardwareType.GpuIntel)
                                    {
                                        var temperature = SelectGPU(hardware.Sensors.Where(s => s.SensorType == SensorType.Temperature).Select(s => (s.Name, s.Value)));
                                        var load = SelectGPU(hardware.Sensors.Where(s => s.SensorType == SensorType.Load).Select(s => (s.Name, s.Value)), true);
                                        if (temperature.HasValue) gpuTemperatures.Add(temperature.Value);
                                        if (load.HasValue) gpuLoads.Add(load.Value);
                                    }
                                }
                                catch (Exception e) { error = e.Message; }
                            }
                            cpu = SelectCPU(cpuSensors);
                            if (gpuTemperatures.Count > 0) gpu = gpuTemperatures.Max();
                            if (gpuLoads.Count > 0) gpuUsage = gpuLoads.Max();
                            if (cpu == null && error == null)
                                error = PawnIo.IsInstalled ? "CPU temperature sensor is unavailable" : "PawnIO driver is not installed";
                        }
                        catch (Exception e)
                        {
                            error = e.Message;
                            computer.Close();
                            opened = false;
                        }
                        cached = JsonSerializer.Serialize(new { cpu, gpu, gpuUsage, error });
                        sampled = Environment.TickCount64;
                    }
                    try
                    {
                        using var writer = new StreamWriter(pipe, new UTF8Encoding(false), leaveOpen: true) { AutoFlush = true };
                        await writer.WriteLineAsync(cached.AsMemory(), stop).WaitAsync(TimeSpan.FromSeconds(2), stop);
                    }
                    catch (IOException) { } // The client may close during application shutdown.
                    catch (System.TimeoutException) { }
                }
            }
            catch (OperationCanceledException) when (stop.IsCancellationRequested) { }
            finally { if (opened) computer.Close(); }
        }
    }

    // Reject a same-name pipe created by another local process.
    private static uint ServicePID()
    {
        IntPtr manager = OpenSCManager(null, null, 1);
        if (manager == IntPtr.Zero) throw new IOException("Cannot query CPU temperature service");
        try
        {
            IntPtr service = OpenService(manager, Name, 4);
            if (service == IntPtr.Zero) throw new IOException("CPU temperature service is not installed");
            try
            {
                var status = new uint[9];
                if (!QueryServiceStatusEx(service, 0, status, 36, out _) || status[1] != 4 || status[7] == 0)
                    throw new IOException("CPU temperature service is not running");
                return status[7];
            }
            finally { CloseServiceHandle(service); }
        }
        finally { CloseServiceHandle(manager); }
    }
    [DllImport("kernel32.dll", SetLastError = true)]
    private static extern bool GetNamedPipeServerProcessId(SafePipeHandle pipe, out uint pid);
    [DllImport("advapi32.dll", CharSet = CharSet.Unicode, SetLastError = true)]
    private static extern IntPtr OpenSCManager(string? machine, string? database, uint access);
    [DllImport("advapi32.dll", CharSet = CharSet.Unicode, SetLastError = true)]
    private static extern IntPtr OpenService(IntPtr manager, string name, uint access);
    [DllImport("advapi32.dll", SetLastError = true)]
    private static extern bool QueryServiceStatusEx(IntPtr service, int level, [Out] uint[] status, int size, out uint needed);
    [DllImport("advapi32.dll")]
    private static extern bool CloseServiceHandle(IntPtr handle);
}
