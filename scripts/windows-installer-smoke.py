"""Install/uninstall the released EXE in a temporary directory with no hardware."""
import pathlib,sys,tempfile,subprocess,os
setup=pathlib.Path(sys.argv[1]).resolve()
with tempfile.TemporaryDirectory(prefix='sharkcool-installer-') as directory:
    app=pathlib.Path(directory)/'App'
    # Do not start the real device service, change the user's autostart, or make
    # shortcuts in a CI worker's profile during this isolated installer check.
    subprocess.run([str(setup),'/VERYSILENT','/SUPPRESSMSGBOXES','/NORESTART','/TASKS=cputemp','/NOICONS',f'/DIR={app}'],check=True,timeout=120)
    for name in ['SharkCool.exe','sharkcool-service.exe','sharkcool-window.exe','sharkcool-bridge.exe','vcruntime140.dll','msvcp140.dll']:
        assert (app/name).is_file(),name
    helper=pathlib.Path(os.environ['ProgramFiles'])/'SharkCoolSensors/sharkcool-sensors.exe'
    assert helper.is_file()
    sample=__import__('json').loads(subprocess.check_output([str(helper),'--read'],text=True,timeout=6))
    assert 'cpu' in sample and 'error' in sample
    subprocess.run([sys.executable,str(pathlib.Path(__file__).with_name('windows-smoke.py')),str(app)],check=True,timeout=50)
    subprocess.run([str(app/'unins000.exe'),'/VERYSILENT','/SUPPRESSMSGBOXES','/NORESTART'],check=True,timeout=60)
    assert not (app/'SharkCool.exe').exists()
    assert not helper.exists()
    print('Downloaded installer installs, runs and uninstalls the complete app')
