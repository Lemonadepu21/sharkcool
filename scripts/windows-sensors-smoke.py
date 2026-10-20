"""First-launch setup, repeat launch, real service IPC and clean removal."""
import json, os, pathlib, re, socket, subprocess, sys, time, urllib.request

package = pathlib.Path(sys.argv[1]).resolve()
install = package / 'Install-Sensors.ps1'
helper = pathlib.Path(os.environ['ProgramFiles']) / 'SharkCoolSensors/sharkcool-sensors.exe'
cleanup = ['powershell.exe', '-NoProfile', '-NonInteractive', '-ExecutionPolicy', 'Bypass', '-File', str(install), '-Uninstall']
http = urllib.request.build_opener(urllib.request.ProxyHandler({}))

def launch_and_stop():
    with socket.socket() as sock:
        sock.bind(('127.0.0.1', 0))
        port = sock.getsockname()[1]
    base = f'http://127.0.0.1:{port}'
    # This exercises the user's normal launcher, including automatic UAC setup,
    # without a GUI, fake temperatures, shortcuts or autostart changes.
    subprocess.run([str(package / 'SharkCool.exe'), 'gui', '--no-browser', '--listen', f'127.0.0.1:{port}'], check=True, timeout=130)
    token = None
    try:
        for _ in range(100):
            try:
                page = http.open(base, timeout=1).read().decode()
                token = re.search(r'name="sharkcool-token" content="([a-f0-9]+)"', page).group(1)
                break
            except OSError:
                time.sleep(.1)
        else:
            raise RuntimeError('First-launch backend did not start')
        assert helper.is_file(), 'First launch did not install the sensor service'
        sample = json.loads(subprocess.check_output([str(helper), '--read'], text=True, timeout=6))
        assert all(key in sample for key in ('cpu', 'gpu', 'gpuUsage', 'error')), sample
        for key in ('cpu', 'gpu'):
            assert sample[key] is None or 0 < sample[key] <= 150, sample
        assert sample['gpuUsage'] is None or 0 <= sample['gpuUsage'] <= 100, sample
        return subprocess.check_output(['powershell.exe', '-NoProfile', '-NonInteractive', '-Command', "(Get-CimInstance Win32_Service -Filter \"Name='SharkCoolSensors'\").ProcessId"], text=True, timeout=10).strip()
    finally:
        if token:
            request = urllib.request.Request(base + '/api/quit', data=b'{}', headers={'Content-Type': 'application/json', 'X-SharkCool-Token': token})
            assert json.load(http.open(request, timeout=5))['quitting']
            for _ in range(50):
                try:
                    http.open(base + '/api/status', timeout=.2).close()
                except OSError:
                    break
                time.sleep(.1)
            else:
                raise RuntimeError('First-launch backend did not exit')

try:
    first_pid = launch_and_stop()
    assert int(first_pid) > 0
    assert launch_and_stop() == first_pid, 'Repeat launch unexpectedly reinstalled/restarted the service'
    subprocess.run(['go', 'test', './internal/sensors', '-run', '^TestInstalledSensorService$', '-count=1'], env={**os.environ, 'SHARKCOOL_TEST_SENSORS': '1'}, check=True, timeout=60)
    print('Automatic first launch, repeat launch without reinstall, trusted native IPC and complete response passed')
finally:
    subprocess.run(cleanup, check=True, timeout=60)
assert not helper.exists()
