'use strict';
const $ = id => document.getElementById(id);
let token = document.querySelector('meta[name="sharkcool-token"]').content;
const effects = {1:'flow',2:'cycle',3:'breathing',4:'static',5:'blinking',6:'reactive',7:'audio',8:'refresh'};
const effectNames = {flow:'彩色流动',cycle:'颜色循环',breathing:'呼吸',static:'常亮',blinking:'闪烁',reactive:'响应',audio:'音频同步',refresh:'刷新'};
const speedBounds = {flow:[1500,4000],cycle:[5000,15000],breathing:[1000,5000],blinking:[450,1050],reactive:[300,1000],refresh:[250,750]};
let busy=false, connected=false, demo=false, connectionKey='', syncing=false, retryAt=0;
let loaded={cooling:false,rgb:false,device:false,lcd:false,presets:false};
let dirty={cooling:false,device:false};
let rgbChanged=new Set();
let presetConfigs={},presetFormat='',activeCooling=null;
let screenBusy=false,screenDraft='',canRestoreScreen=false;
const editorFields=['cooling-format','cooling-rpm','speed-slider','cooling-slot','cooling-source','lighting-effect','lighting-color-mode','lighting-color','lighting-brightness','lighting-speed',...[1,2,3,4].flatMap(i=>['curve-temp-'+i,'curve-rpm-'+i]),...[1,2,3].map(i=>'lcd-row-'+i)];
async function api(path,body,retried=false) {
 const options=body===undefined?{}:{method:'POST',headers:{'Content-Type':'application/json','X-SharkCool-Token':token},body:JSON.stringify(body)};
 const response=await fetch(path,{...options,signal:AbortSignal.timeout(7000)});
 const data=await response.json();
 if(response.status===403&&data.error==='缺少有效的会话令牌'&&!retried){const page=await fetch('/',{signal:AbortSignal.timeout(7000)});const html=await page.text();const match=html.match(/name="sharkcool-token" content="([a-f0-9]+)"/);if(match){token=match[1];return api(path,body,true);}}
 if(!response.ok)throw new Error(data.error||'请求失败');return data;
}
function buttons() {
 const locked=busy||screenBusy;
 editorFields.forEach(id=>{$(id).disabled=locked;});
 document.querySelectorAll('[data-slot]').forEach(b=>{b.disabled=locked||!connected||(!demo&&!presetConfigs[b.dataset.slot]);});
 document.querySelectorAll('[data-switch], #set-lcd-layout, #read-cooling-preset').forEach(b=>{b.disabled=locked||!connected||demo;});
 $('set-lcd-layout').disabled=locked||!connected||demo||[1,2,3].some(i=>!$('lcd-row-'+i).value);
 document.querySelectorAll('[data-switch]').forEach(b=>{if(b.dataset.switch==='lcd')b.disabled=locked||!connected||demo||!loaded.lcd;});
 $('save-cooling').disabled=locked||!connected||(!demo&&!loaded.cooling)||(demo&&$('cooling-format').value==='curve');
 $('set-lighting').disabled=locked||!connected||demo||!loaded.rgb||['','audio','reactive'].includes($('lighting-effect').value);
 $('set-device-options').disabled=locked||!connected||demo||!loaded.device;
 ['device-smart','device-power-on'].forEach(id=>{$(id).disabled=locked||!connected||demo||!loaded.device;});
 $('system-info-enabled').disabled=locked||demo;
 $('screen-file').disabled=locked;
 $('apply-screen').disabled=locked||!connected||demo||!screenDraft;
 $('restore-screen').disabled=locked||!connected||demo||!canRestoreScreen;
}
function resetConnection(key) {
 connectionKey=key;loaded={cooling:false,rgb:false,device:false,lcd:false,presets:false};dirty={cooling:false,device:false};rgbChanged.clear();retryAt=0;presetConfigs={};presetFormat='';activeCooling=null;
 [1,2,3].forEach(i=>{$('lcd-row-'+i).value='';});$('lcd-layout-state').textContent='当前布局无法读取';
 showLCDSwitch(null);renderPresets();
 $('cooling-current').textContent=key?'正在读取当前配置…':'等待设备连接';
 $('lighting-effect').value='';$('lighting-state').textContent=key?'正在识别当前灯效…':'等待设备连接';
 $('device-options-state').textContent=key?'正在读取启动选项…':'等待设备连接';
}
async function syncDevice(key) {
 if(syncing||busy||screenBusy||demo||!key||Date.now()<retryAt)return;
 syncing=true;retryAt=Date.now()+5000;
 try {
  for(const kind of ['cooling','rgb','device','lcd']) {
   if(key!==connectionKey||!connected||busy)return;
   if(loaded[kind]&&!['cooling','lcd'].includes(kind))continue;
   try {
    const data=await api('/api/query',{kind:kind==='lcd'?'lcd-switch':kind});
    if(key!==connectionKey||!connected||busy)return;
    if(kind==='cooling') {if(!data.config)throw new Error('未返回散热配置');showCurrentCooling(data.config);if(!dirty.cooling)showCooling(data.config);}
    if(kind==='rgb') {if(!data.config||!effects[data.config.effect])throw new Error('无法识别当前灯效');if(!rgbChanged.size)showLighting(data.config);else lightingState(data.config);}
    if(kind==='device') {if(!data.options)throw new Error('未返回启动选项');if(!dirty.device)showDeviceOptions(data.options);}
    if(kind==='lcd'){if(typeof data.enabled!=='boolean')throw new Error('无法识别屏幕开关');showLCDSwitch(data.enabled);}
    loaded[kind]=true;
   }catch(e){if(key!==connectionKey)return;if(kind==='lcd'){loaded.lcd=false;showLCDSwitch(null);}$(kind==='cooling'?'cooling-current':kind==='rgb'?'lighting-state':kind==='lcd'?'lcd-switch-state':'device-options-state').textContent='读取失败，将自动重试：'+e.message;}
  }
  if(!loaded.presets&&key===connectionKey&&connected&&!busy)await syncPresets(key);
 }finally{syncing=false;buttons();}
}
async function refresh() {
 try {
  const s=await api('/api/status');connected=s.connected;demo=s.demo;
  const key=connected&&!demo?[s.session,s.device,s.transport,s.connectionGeneration].join('|'):'';
  if(key!==connectionKey)resetConnection(key);
  $('connection').textContent=demo?'演示模式':connected?(s.stale?'等待心跳':'已连接'):'未连接';
  $('connection').classList.toggle('connected',connected&&!s.stale);
  $('demo').classList.toggle('hidden',!demo);
  $('rpm').textContent=s.stale||!connected?'—':s.rpm;$('rpmbar').value=s.stale||!connected?0:s.rpm;
  $('cpu').textContent=s.temps.cpu??'—';$('gpu').textContent=s.temps.gpu??'—';
 $('device').textContent=connected?(s.transport==='ble'?'蓝牙':'USB'):'未连接';
  $('error').textContent=s.error;$('error').classList.toggle('hidden',!s.error);
  $('autostart').disabled=demo;
  $('cooling-rpm').max=demo?3600:4000;$('speed-slider').max=demo?3600:4000;$('speed-max').textContent=(demo?3600:4000)+' RPM';
  if(demo){$('cooling-current').textContent='演示控制 · 不访问散热器';$('lighting-state').textContent='演示模式不读取或修改灯效。';$('device-options-state').textContent='演示模式不读取或修改设备选项。';}
  buttons();void syncDevice(key);
 }catch(e){connected=false;resetConnection('');buttons();$('autostart').disabled=true;$('connection').textContent='服务已断开';$('connection').classList.remove('connected');$('rpm').textContent='—';$('rpmbar').value=0;$('cpu').textContent='—';$('gpu').textContent='—';$('error').textContent='无法连接本机 SharkCool 服务，请重新启动程序。';$('error').classList.remove('hidden');}
}
async function action(path,body,state='result',onSuccess=()=>{}) {
 if(busy||screenBusy)return;
 const actionKey=connectionKey;busy=true;buttons();$(state).textContent='应用中…';
 try{const payload=typeof body==='function'?await body():body;if(actionKey!==connectionKey)throw new Error('连接已变化，请重试。');const data=await api(path,payload);if(actionKey!==connectionKey)throw new Error('连接已变化，请重试。');$(state).textContent=data.active===false?'已保存，尚未生效':data.confirmed===false||data.acknowledged===false?'未确认，请重试':'已应用';onSuccess(data);}
 catch(e){$(state).textContent=e.message;}
 finally{busy=false;await refresh();buttons();}
}
function coolingFields() {
 const curve=$('cooling-format').value==='curve';
 $('cooling-points').classList.toggle('hidden',!curve);$('cooling-rpm-field').classList.toggle('hidden',curve);$('fixed-controls').classList.toggle('hidden',curve);$('curve-note').classList.toggle('hidden',!curve);
 $('save-cooling').textContent=curve?'保存温控曲线':'应用转速';buttons();
}
function showCurrentCooling(c){activeCooling=c;$('cooling-current').textContent='当前：'+c.slot+' 档 · '+coolingDescription(c);renderPresets();}
function renderPresets(){document.querySelectorAll('[data-slot]').forEach(b=>{const c=presetConfigs[b.dataset.slot];b.textContent=b.dataset.slot+' 档'+(demo?' · '+b.dataset.rpm:c?' · '+coolingDescription(c):'');b.classList.toggle('active',!!activeCooling&&activeCooling.slot===Number(b.dataset.slot)&&Boolean(activeCooling.points)===(presetFormat==='curve'));});}
async function syncPresets(key){
 const format=$('cooling-format').value;const configs={};
 try{for(const slot of [1,2,3,4]){if(key!==connectionKey||busy)return;const data=await api('/api/query',{kind:'cooling-preset',curve:format==='curve',slot});if(key!==connectionKey||busy||format!==$('cooling-format').value)return;if(!data.config||data.config.slot!==slot)throw Error('档位配置无效');configs[slot]=data.config;}presetConfigs=configs;presetFormat=format;loaded.presets=true;renderPresets();}
 catch(e){if(key===connectionKey)$('cooling-state').textContent='档位读取失败，将自动重试：'+e.message;}
}
function invalidatePresets(){loaded.presets=false;presetConfigs={};presetFormat='';retryAt=0;renderPresets();}
function coolingDescription(c) {return c.points?'温控曲线 · '+(c.source===0?'CPU':'GPU'):c.rpm+' RPM';}
function updateSpeed(value) {
 $('cooling-rpm').value=value;$('speed-slider').value=value;
}
function showCooling(c) {
 const format=c.points?'curve':'fixed';if($('cooling-format').value!==format)invalidatePresets();$('cooling-format').value=format;$('cooling-slot').value=c.slot;$('cooling-source').value=c.source===0?'cpu':'gpu';
 if(c.points)c.points.forEach((p,i)=>{$('curve-temp-'+(i+1)).value=p.temperature;$('curve-rpm-'+(i+1)).value=p.rpm;});else updateSpeed(c.rpm);
 coolingFields();
}
$('cooling-format').addEventListener('change',()=>{dirty.cooling=true;invalidatePresets();coolingFields();void syncDevice(connectionKey);});
$('cooling-rpm').addEventListener('input',()=>{dirty.cooling=true;updateSpeed($('cooling-rpm').value);});
$('speed-slider').addEventListener('input',()=>{dirty.cooling=true;updateSpeed($('speed-slider').value);});
document.querySelectorAll('[data-slot]').forEach(b=>b.addEventListener('click',()=>{
 if(b.disabled||busy||screenBusy||!connected)return;
 const slot=Number(b.dataset.slot),curve=$('cooling-format').value==='curve';
 action(demo?'/api/speed':'/api/cooling',demo?{rpm:Number(b.dataset.rpm)}:async()=>{const data=await api('/api/query',{kind:'cooling-preset',curve,slot});if(!data.config||data.config.slot!==slot||Boolean(data.config.points)!==curve)throw Error('设备返回的档位配置不匹配');return data.config;},'cooling-state',data=>{dirty.cooling=false;loaded.cooling=false;retryAt=0;if(data.active&&data.config){showCooling(data.config);showCurrentCooling(data.config);}else if(demo)updateSpeed(b.dataset.rpm);});
}));
['cooling-slot','cooling-source',...[1,2,3,4].flatMap(i=>['curve-temp-'+i,'curve-rpm-'+i])].forEach(id=>$(id).addEventListener('change',()=>{dirty.cooling=true;}));
async function loadEditingPreset(){
 if(busy||screenBusy||!connected||demo)return;busy=true;buttons();
 try{const data=await api('/api/query',{kind:'cooling-preset',curve:$('cooling-format').value==='curve',slot:Number($('cooling-slot').value)});if(!data.config)throw new Error('未返回配置');showCooling(data.config);dirty.cooling=true;$('cooling-state').textContent='已载入 '+coolingDescription(data.config)+'，应用后才会更改设备。';}
 catch(e){$('cooling-state').textContent=e.message;}finally{busy=false;buttons();}
}
$('read-cooling-preset').addEventListener('click',loadEditingPreset);
$('cooling-slot').addEventListener('change',()=>{dirty.cooling=true;void loadEditingPreset();});
$('save-cooling').addEventListener('click',()=>{
 const body={source:$('cooling-source').value==='cpu'?0:1,slot:Number($('cooling-slot').value)};
 if($('cooling-format').value==='curve')body.points=[1,2,3,4].map(i=>({temperature:Number($('curve-temp-'+i).value),rpm:Number($('curve-rpm-'+i).value)}));else body.rpm=Number($('cooling-rpm').value);
 if(!body.points&&(!Number.isInteger(body.rpm)||body.rpm<1200||body.rpm>(demo?3600:4000))){$('cooling-state').textContent='请输入 1200～'+(demo?3600:4000)+' 的整数转速。';return;}
 if(!demo&&!body.points){
  if(!loaded.presets){$('cooling-state').textContent='档位尚未读取，请稍候';return;}
  const lower=presetConfigs[body.slot-1]?.rpm,upper=presetConfigs[body.slot+1]?.rpm;
  if(lower!==undefined&&body.rpm<=lower){$('cooling-state').textContent=body.slot+' 档转速需高于 '+(body.slot-1)+' 档的 '+lower+' RPM';return;}
  if(upper!==undefined&&body.rpm>=upper){$('cooling-state').textContent=body.slot+' 档转速需低于 '+(body.slot+1)+' 档的 '+upper+' RPM';return;}
 }
 action(demo?'/api/speed':'/api/cooling',demo?{rpm:body.rpm}:body,'cooling-state',data=>{dirty.cooling=false;if(data.active&&data.config){showCooling(data.config);showCurrentCooling(data.config);invalidatePresets();}else if(!demo){loaded.cooling=false;retryAt=0;}});
});
function lightingFields() {
 const bounds=speedBounds[$('lighting-effect').value];$('lighting-speed-field').classList.toggle('hidden',!bounds);$('lighting-speed-value').textContent=$('lighting-speed').value+'%';$('lighting-brightness-value').textContent=$('lighting-brightness').value+'%';buttons();
 $('lighting-color-field').classList.toggle('hidden',$('lighting-color-mode').value!=='single');
}
function lightingState(c) {$('lighting-state').textContent='';}
function showLighting(c) {
 $('lighting-effect').value=effects[c.effect];$('lighting-color-mode').value=c.colorCount===10?'multi':'single';$('lighting-color').value='#'+c.color.map(v=>v.toString(16).padStart(2,'0')).join('');$('lighting-brightness').value=c.brightness;
 const bounds=speedBounds[effects[c.effect]];
 if(bounds)$('lighting-speed').value=Math.max(0,Math.min(100,Math.round(100*(bounds[1]-c.interval)/(bounds[1]-bounds[0]))));
 lightingFields();lightingState(c);
}
for(const field of ['effect','color','brightness','speed'])$('lighting-'+field).addEventListener('input',()=>{if(field==='effect'&&!rgbChanged.has('speed'))$('lighting-speed').value=50;rgbChanged.add(field);lightingFields();});
$('lighting-color-mode').addEventListener('change',()=>{rgbChanged.add('colorMode');if($('lighting-color-mode').value==='multi')rgbChanged.delete('color');lightingFields();});
$('set-lighting').addEventListener('click',()=>{
 const body={};for(const field of rgbChanged){if(field==='speed'&&!speedBounds[$('lighting-effect').value])continue;if(field==='color'&&$('lighting-color-mode').value==='multi')continue;body[field]=field==='colorMode'?$('lighting-color-mode').value:['brightness','speed'].includes(field)?Number($('lighting-'+field).value):$('lighting-'+field).value;}
 if(!Object.keys(body).length)body.brightness=Number($('lighting-brightness').value);
 action('/api/rgb',body,'lighting-state',data=>{showLighting(data.config);rgbChanged.clear();$('lighting-state').textContent='已应用';});
});
function showDeviceOptions(o) {
 $('device-smart').checked=o.smartStartStop;$('device-power-on').checked=o.powerOnStart;
 $('device-options-state').textContent='';
}
['device-smart','device-power-on'].forEach(id=>$(id).addEventListener('change',()=>{dirty.device=true;}));
$('set-device-options').addEventListener('click',()=>action('/api/device-options',{smartStartStop:$('device-smart').checked,powerOnStart:$('device-power-on').checked},'device-options-state',data=>{showDeviceOptions(data.options);dirty.device=false;$('device-options-state').textContent='已保存';}));
function showLCDSwitch(on){document.querySelectorAll('[data-switch]').forEach(b=>{if(b.dataset.switch==='lcd'){const active=typeof on==='boolean'&&on===(b.dataset.on==='true');b.classList.toggle('active',active);b.setAttribute('aria-pressed',String(active));}});$('lcd-switch-state').textContent=on===null?'屏幕状态未读取':'';}
document.querySelectorAll('[data-switch]').forEach(b=>b.addEventListener('click',()=>action('/api/switch',{kind:b.dataset.switch,on:b.dataset.on==='true'},'result',()=>{if(b.dataset.switch==='lcd'){loaded.lcd=false;retryAt=0;showLCDSwitch(null);}})));
$('autostart').addEventListener('change',async()=>{const on=$('autostart').checked;await action('/api/autostart',{enabled:on},'result',s=>{$('autostart').checked=s.autostart;});try{const s=await api('/api/settings');$('autostart').checked=s.autostart;}catch(e){$('result').textContent=e.message;}});
const metricNames={'cpu-temperature':'CPU 温度','gpu-temperature':'GPU 温度','cpu-usage':'CPU 利用率','gpu-usage':'GPU 利用率','fan-rpm':'风扇转速','disk-usage':'磁盘使用率','memory-usage':'内存使用率','time':'时钟'};
for(let row=1;row<=3;row++){const select=$('lcd-row-'+row);const unknown=document.createElement('option');unknown.value='';unknown.textContent='选择显示内容';unknown.disabled=true;select.append(unknown);select.addEventListener('change',buttons);for(const [value,label] of Object.entries(metricNames)){const option=document.createElement('option');option.value=value;option.textContent=label;select.append(option);}select.value='';}
$('set-lcd-layout').addEventListener('click',()=>{const rows=[1,2,3].map(row=>$('lcd-row-'+row).value);if(rows.some(v=>!v)||new Set(rows).size!==3){$('result').textContent='请选择三个不同指标。';return;}action('/api/lcd-layout',{rows},'lcd-layout-state',()=>{$('lcd-layout-state').textContent='已发送布局，请核对屏幕';});});
function formatMetric(metric,value){if(metric==='time')return String(Math.floor(value/60)).padStart(2,'0')+':'+String(value%60).padStart(2,'0');return value+(metric.endsWith('temperature')?' °C':'%');}
async function refreshSystemInfo(){try{const s=await api('/api/system-info');if(!busy)$('system-info-enabled').checked=s.enabled;$('system-info-values').textContent=Object.keys(metricNames).filter(k=>k!=='fan-rpm').map(k=>metricNames[k]+' '+(s.values[k]===undefined?'不可用':formatMetric(k,s.values[k]))).join(' · ');$('system-info-status').textContent=s.error?'推送失败：'+s.error:s.enabled?'同步中':'已关闭';}catch(e){$('system-info-status').textContent=e.message;$('system-info-enabled').disabled=true;}}
$('system-info-enabled').addEventListener('change',async()=>{const enabled=$('system-info-enabled').checked;await action('/api/system-info',{enabled});await refreshSystemInfo();});
async function poll(){await refreshScreen();await refresh();await refreshSystemInfo();setTimeout(poll,1000);}
api('/api/settings').then(s=>{$('autostart').checked=s.autostart;}).catch(e=>{$('result').textContent=e.message;});
$('lighting-effect').value='';coolingFields();lightingFields();poll();

$('screen-file').addEventListener('change',async()=>{
 const file=$('screen-file').files[0];if(!file||busy||screenBusy)return;
 screenDraft='';busy=true;buttons();$('screen-state').textContent='准备预览…';
 try{
  if(file.size>8*1024*1024)throw Error('请选择 8 MB 以内的图片。');
  const bytes=new Uint8Array(await file.arrayBuffer());let binary='';
  for(let i=0;i<bytes.length;i+=32768)binary+=String.fromCharCode(...bytes.subarray(i,i+32768));
  const data=await api('/api/screensaver/prepare',{image:btoa(binary)});
  screenDraft=data.draftId;$('screen-preview').src='data:image/png;base64,'+data.preview;$('screen-preview').classList.remove('hidden');$('screen-state').textContent='';
 }catch(e){$('screen-state').textContent=e.message;$('screen-preview').classList.add('hidden');}
 finally{busy=false;buttons();}
});
async function refreshScreen(){
 try{const s=await api('/api/screensaver');screenBusy=s.running;canRestoreScreen=s.canRestore;if(screenDraft&&screenDraft!==s.draftId)screenDraft='';
  $('screen-transfer').classList.toggle('hidden',!s.running);if(s.progress<0)$('screen-progress').removeAttribute('value');else $('screen-progress').value=s.progress;
  if(s.running||s.phase)$('screen-state').textContent=s.error||s.phase+(s.running&&s.progress>=0?' · '+s.progress+'%':'');buttons();
 }catch(e){$('screen-state').textContent='无法读取屏保状态';screenBusy=false;canRestoreScreen=false;screenDraft='';buttons();}
}
async function applyScreen(restore=false){
 if(busy||screenBusy)return;busy=true;buttons();
 try{await api(restore?'/api/screensaver/restore':'/api/screensaver/apply',restore?{}:{draftId:screenDraft});await refreshScreen();}
 catch(e){$('screen-state').textContent=e.message;}finally{busy=false;buttons();}
}
$('apply-screen').addEventListener('click',()=>applyScreen());
$('restore-screen').addEventListener('click',()=>applyScreen(true));
