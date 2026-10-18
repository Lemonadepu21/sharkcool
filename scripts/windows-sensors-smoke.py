"""Check the signed driver installer, CPU service and authenticated local IPC."""
import json, os, pathlib, subprocess, sys, time

package = pathlib.Path(sys.argv[1]).resolve()
install = package / 'Install-Sensors.ps1'
helper = pathlib.Path(os.environ['ProgramFiles']) / 'SharkCoolSensors/sharkcool-sensors.exe'
command = ['powershell.exe', '-NoProfile', '-NonInteractive', '-ExecutionPolicy', 'Bypass', '-File', str(install)]
try:
    subprocess.run(command, check=True, timeout=90)
    for attempt in range(10):
        result = subprocess.run([str(helper), '--read'], capture_output=True, text=True, timeout=6)
        if result.returncode == 0:
            sample = json.loads(result.stdout)
            assert 'cpu' in sample and 'error' in sample, sample
            assert sample['cpu'] is None or 0 < sample['cpu'] <= 150, sample
            assert sample['cpu'] is not None or sample['error'], sample
            break
        time.sleep(.3)
    else:
        raise RuntimeError(result.stderr)
    print('CPU service install, authenticated local pipe read and missing-driver handling passed')
finally:
    subprocess.run(command + ['-Uninstall'], check=True, timeout=60)
assert not helper.exists()
