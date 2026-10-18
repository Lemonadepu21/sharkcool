"""Exercise the shipped Windows app, without a cooler or pairing changes."""
import pathlib, subprocess, sys, time, urllib.request, json, re, socket, os
package=pathlib.Path(sys.argv[1]).resolve()
# A random loopback port prevents touching an already-running installation.
with socket.socket() as sock:
    sock.bind(('127.0.0.1',0)); port=sock.getsockname()[1]
base=f'http://127.0.0.1:{port}'
http=urllib.request.build_opener(urllib.request.ProxyHandler({}))
with (package/'smoke.log').open('wb') as log:
    service=subprocess.Popen([str(package/'sharkcool-service.exe'),'gui','--demo','--no-browser','--listen',f'127.0.0.1:{port}'],stdout=log,stderr=log)
    window=None
    try:
        for _ in range(80):
            try:
                status=json.load(http.open(base+'/api/status',timeout=1))
                if status['demo']:break
            except OSError:pass
            if service.poll() is not None:raise RuntimeError('Service exited early')
            time.sleep(.1)
        else:raise RuntimeError('Packaged service did not start')
        assert status['version'].endswith('-windows'),status
        page=http.open(base,timeout=3).read().decode()
        assert re.search(r'name="sharkcool-token" content="[a-f0-9]+"',page)
        preview=package/'smoke-preview.png'
        subprocess.run([str(package/'sharkcool-window.exe'),'--url',base,'--render-preview',str(preview)],check=True,timeout=15)
        assert preview.read_bytes().startswith(b'\x89PNG\r\n\x1a\n')
        # The no-console launcher must connect to this service, create a real
        # Qt window, and leave the backend running when the window closes.
        launcher=subprocess.run([str(package/'SharkCool.exe'),'gui','--demo','--listen',f'127.0.0.1:{port}'],check=True,timeout=10)
        time.sleep(1.5)
        if os.name == 'nt':
            inventory = "$p=Get-CimInstance Win32_Process -Filter \"Name='sharkcool-window.exe'\";@($p | Where-Object { $_.CommandLine -match $env:SHARKCOOL_SMOKE_URL }).Count"
            count = subprocess.check_output(['powershell.exe','-NoProfile','-NonInteractive','-Command',inventory],env={**os.environ,'SHARKCOOL_SMOKE_URL':re.escape(base)},timeout=10,text=True)
            assert int(count.strip()) == 1, ('Expected one native window', count)
        artifact = os.environ.get('SHARKCOOL_SMOKE_ARTIFACT')
        if artifact:
            import shutil
            shutil.copyfile(preview, artifact)
        assert service.poll() is None
        assert json.load(http.open(base+'/api/status',timeout=3))['demo']
        token=re.search(r'name="sharkcool-token" content="([a-f0-9]+)"',page).group(1)
        request=urllib.request.Request(base+'/api/quit',data=b'{}',headers={'Content-Type':'application/json','X-SharkCool-Token':token})
        assert json.load(http.open(request,timeout=3))['quitting']
        assert service.wait(timeout=6)==0
        print('Packaged Windows backend, HTTP identity, Qt preview, launcher and graceful shutdown passed')
    finally:
        # Close only the window attached to our random-port fixture, not any
        # real installation. Its normalized URL is visible in the command line.
        if os.name=='nt':
            script="$p=Get-CimInstance Win32_Process -Filter \"Name='sharkcool-window.exe'\";foreach($x in $p){if($x.CommandLine -match $env:SHARKCOOL_SMOKE_URL){Stop-Process -Id $x.ProcessId -ErrorAction SilentlyContinue}}"
            subprocess.run(['powershell.exe','-NoProfile','-NonInteractive','-Command',script],env={**os.environ,'SHARKCOOL_SMOKE_URL':re.escape(base)},check=True,timeout=10)
        if service.poll() is None:service.terminate()
        service.wait(timeout=10)
        log.close()  # Windows cannot unlink an open file.
        for name in ['smoke.log','smoke-preview.png']:
            (package/name).unlink(missing_ok=True)
