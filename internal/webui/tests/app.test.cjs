const {test}=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs');
const vm=require('node:vm');
const html=fs.readFileSync(__dirname+'/../assets/index.html','utf8');
const source=fs.readFileSync(__dirname+'/../assets/app.js','utf8');
class Element {
 constructor(value=''){this.value=value;this.checked=false;this.disabled=false;this.textContent='';this.dataset={};this.listeners={};this.classes=new Set();this.classList={toggle:(k,on)=>on?this.classes.add(k):this.classes.delete(k),remove:k=>this.classes.delete(k)};}
 addEventListener(type,fn){this.listeners[type]=fn;}
 append(){}
 setAttribute(){}
 removeAttribute(){}
 async emit(type){return this.listeners[type]?.();}
}
const settle=async()=>{for(let i=0;i<12;i++)await new Promise(resolve=>setImmediate(resolve));};
function page({demo=false,failDevice=false,holdRGB=false}={}) {
 const nodes=new Map([...html.matchAll(/id="([^"]+)"/g)].map(m=>[m[1],new Element()]));
 nodes.get('cooling-format').value='fixed';nodes.get('cooling-slot').value='1';nodes.get('cooling-source').value='cpu';nodes.get('cooling-rpm').value='1800';nodes.get('lighting-effect').value='';nodes.get('lighting-speed').value='50';
 const presets=[1200,2134,2912,3534].map(rpm=>{const b=new Element();b.dataset.rpm=String(rpm);b.dataset.slot=String([1200,2134,2912,3534].indexOf(rpm)+1);return b;});
 const switches=['rgb','lcd'].flatMap(kind=>[true,false].map(on=>{const b=new Element();b.dataset={switch:kind,on:String(on)};return b;}));
 const calls=[];let pendingRGB;const screen={draftId:'',running:false,phase:'',progress:0,error:'',confirmed:false,canRestore:false};
 const state={connected:true,connectionGeneration:1,device:'cooler',transport:'ble',stale:false,rpm:1400,temps:{cpu:41,gpu:36},demo,error:'',lcd:true,presets:[1200,2134,2912,3534],options:{smartStartStop:true,powerOnStart:false},rgb:{effect:8,interval:500,brightness:80,colorCount:10,color:[0,255,72]},cooling:{source:1,slot:1,rpm:1200}};
 const document={getElementById:id=>{assert.ok(nodes.has(id),'Missing HTML element '+id);return nodes.get(id);},createElement:()=>new Element(),querySelector:()=>({content:'token'}),querySelectorAll:selector=>selector.split(',').flatMap(s=>{s=s.trim();if(s==='[data-slot]')return presets;if(s==='[data-switch]')return switches;if(s.startsWith('#'))return nodes.has(s.slice(1))?[nodes.get(s.slice(1))]:[];throw Error('Unsupported selector '+s);})};
 const context=vm.createContext({document,AbortSignal,Date,Set,console,setTimeout:()=>{},fetch:async(path,options)=>{
  const body=options.body?JSON.parse(options.body):undefined;calls.push({path,body});let data;
  if(path==='/api/status')data={...state};
  else if(path==='/api/screensaver')data={...screen};
  else if(path==='/api/screensaver/apply'){screen.running=true;screen.phase='备份原图';screen.progress=-1;data={...screen};}
  else if(path==='/api/settings')data={autostart:false};
  else if(path==='/api/system-info')data={enabled:false,values:{},error:''};
  else if(path==='/api/query'){
   assert.equal(demo,false,'Demo must never query hardware');
   if(body.kind==='device'&&failDevice)return{ok:false,json:async()=>({error:'read failed'})};
   if(body.kind==='rgb'&&holdRGB){holdRGB=false;return new Promise(resolve=>{pendingRGB=()=>resolve({ok:true,json:async()=>({config:{...state.rgb,effect:1}})});});}
   data=body.kind==='lcd-switch'?{enabled:state.lcd}:body.kind==='cooling-preset'?{config:{source:0,slot:body.slot,rpm:state.presets[body.slot-1]}}:body.kind==='device'?{options:state.options}:{config:state[body.kind==='rgb'?'rgb':'cooling']};
  }else if(path==='/api/rgb'){if(body.colorMode)state.rgb.colorCount=body.colorMode==='multi'?10:1;data={config:state.rgb,message:'verified'};}
  else if(path==='/api/cooling'){state.cooling=body;data={config:body,active:true,message:'verified'};}
  else if(path==='/api/lcd-layout')data={acknowledged:true};
  else if(path==='/api/speed')data={message:'simulated'};
  else throw Error('Unexpected API '+path);
  return{ok:true,json:async()=>data};
 }});
 vm.runInContext(source,context);
 return{nodes,presets,calls,state,screen,run:s=>vm.runInContext(s,context),releaseRGB:()=>pendingRGB(),retryDevice:()=>{failDevice=false;vm.runInContext('retryAt=0',context);}};
}
test('connect reads all settings and temperatures, repeated polling preserves edits',async()=>{
 const p=page();await settle();
 assert.equal(p.nodes.get('lighting-effect').value,'refresh');assert.equal(p.nodes.get('lighting-speed').value,50);
 assert.equal(p.nodes.get('device-smart').checked,true);assert.equal(p.nodes.get('device-power-on').checked,false);assert.equal(p.nodes.get('device-smart').disabled,false);
 assert.equal(p.nodes.get('cooling-rpm').value,1200);assert.equal(p.nodes.get('cooling-source').value,'gpu');assert.equal(p.nodes.get('cpu').textContent,41);assert.equal(p.nodes.get('gpu').textContent,36);
 const count=p.calls.filter(c=>c.path==='/api/query').length;
 p.nodes.get('lighting-brightness').value='42';await p.nodes.get('lighting-brightness').emit('input');await p.run('refresh()');await settle();
 assert.equal(p.calls.filter(c=>c.path==='/api/query').length,count);assert.equal(p.nodes.get('lighting-brightness').value,'42');
 await p.nodes.get('set-lighting').emit('click');await settle();
 assert.deepEqual(p.calls.find(c=>c.path==='/api/rgb').body,{brightness:42});
});
test('idle status polling leaves the device quiet so its ten second screensaver can start',async()=>{
 const p=page();await settle();
 const count=p.calls.filter(c=>c.path==='/api/query').length;
 for(let i=0;i<3;i++){
  await p.run('retryAt=0;refresh()');await settle();
 }
 assert.equal(p.calls.filter(c=>c.path==='/api/query').length,count,'status polling keeps waking the cooler');
});
test('device slots apply immediately and manual target still uses apply',async()=>{
 const p=page();await settle();await p.presets[2].emit('click');await settle();assert.equal(p.nodes.get('cooling-rpm').value,2912);assert.deepEqual(p.calls.find(c=>c.path==='/api/cooling').body,{source:0,slot:3,rpm:2912});assert.equal(p.presets[2].classes.has('active'),true);
 p.nodes.get('cooling-rpm').value='2300';await p.nodes.get('cooling-rpm').emit('input');assert.equal(p.nodes.get('speed-slider').value,'2300');
 await p.nodes.get('save-cooling').emit('click');await settle();assert.deepEqual(p.calls.filter(c=>c.path==='/api/cooling')[1].body,{source:0,slot:3,rpm:2300});
});
test('color mode follows readback and multi sends no stale single color',async()=>{
 const p=page();await settle();
 assert.equal(p.nodes.get('lighting-color-mode').value,'multi');
 assert.equal(p.nodes.get('lighting-color-field').classes.has('hidden'),true);
 p.nodes.get('lighting-color-mode').value='single';await p.nodes.get('lighting-color-mode').emit('change');
 assert.equal(p.nodes.get('lighting-color-field').classes.has('hidden'),false);
 await p.nodes.get('set-lighting').emit('click');await settle();
 assert.deepEqual(p.calls.find(c=>c.path==='/api/rgb').body,{colorMode:'single'});
 p.nodes.get('lighting-color').value='#123456';await p.nodes.get('lighting-color').emit('input');
 p.nodes.get('lighting-color-mode').value='multi';await p.nodes.get('lighting-color-mode').emit('change');
 await p.nodes.get('set-lighting').emit('click');await settle();
 assert.deepEqual(p.calls.filter(c=>c.path==='/api/rgb')[1].body,{colorMode:'multi'});
 assert.equal(p.nodes.get('lighting-color-mode').value,'multi');
 assert.equal(p.nodes.get('lighting-color-field').classes.has('hidden'),true);
});
test('same device reconnect re-reads, disconnected temperatures remain available',async()=>{
 const p=page();await settle();p.state.connected=false;await p.run('refresh()');assert.equal(p.nodes.get('device-smart').disabled,true);assert.equal(p.nodes.get('cpu').textContent,41);
 p.state.connected=true;p.state.connectionGeneration=2;p.state.rgb.effect=4;p.state.options.smartStartStop=false;
 await p.run('refresh()');await settle();assert.equal(p.nodes.get('lighting-effect').value,'static');assert.equal(p.nodes.get('device-smart').checked,false);assert.equal(p.calls.filter(c=>c.path==='/api/query').length,16);
});
test('failed startup read retries without allowing default device options to be written',async()=>{
 const p=page({failDevice:true});await settle();assert.equal(p.nodes.get('set-device-options').disabled,true);assert.match(p.nodes.get('device-options-state').textContent,/自动重试/);
 p.retryDevice();await p.run('refresh()');await settle();assert.equal(p.nodes.get('set-device-options').disabled,false);assert.equal(p.calls.filter(c=>c.path==='/api/query'&&c.body.kind==='rgb').length,1);
});
test('late response from previous connection is discarded',async()=>{
 const p=page({holdRGB:true});await settle();p.state.connected=false;await p.run('refresh()');p.state.connected=true;p.state.connectionGeneration=2;await p.run('refresh()');p.releaseRGB();await settle();
 assert.equal(p.nodes.get('lighting-effect').value,'');await p.run('refresh()');await settle();assert.equal(p.nodes.get('lighting-effect').value,'refresh');
});
test('host driven effect is identified and requires selection of a supported effect',async()=>{
 const p=page();await settle();p.state.rgb.effect=7;p.state.connectionGeneration++;await p.run('refresh()');await settle();assert.equal(p.nodes.get('lighting-effect').value,'audio');assert.equal(p.nodes.get('set-lighting').disabled,true);
 p.nodes.get('lighting-effect').value='static';await p.nodes.get('lighting-effect').emit('input');assert.equal(p.nodes.get('set-lighting').disabled,false);assert.equal(p.nodes.get('lighting-speed-field').classes.has('hidden'),true);
});
test('demo has no hardware queries and retains real host temperatures',async()=>{
 const p=page({demo:true});await settle();assert.equal(p.calls.some(c=>c.path==='/api/query'),false);assert.equal(p.nodes.get('cpu').textContent,41);assert.equal(p.nodes.get('set-device-options').disabled,true);
 await p.presets[1].emit('click');await p.nodes.get('save-cooling').emit('click');await settle();assert.deepEqual(p.calls.find(c=>c.path==='/api/speed').body,{rpm:2134});
});

test('screen upload disables controls and waits for completed verification',async()=>{
 const p=page();await settle();p.screen.draftId='draft';await p.run("screenDraft='draft';buttons()");
 assert.equal(p.nodes.get('apply-screen').disabled,false);await p.nodes.get('apply-screen').emit('click');await settle();
 assert.equal(p.nodes.get('save-cooling').disabled,true);assert.equal(p.nodes.get('set-lighting').disabled,true);assert.equal(p.nodes.get('screen-file').disabled,true);
 assert.equal(p.nodes.get('screen-state').textContent,'备份原图');
 p.screen.phase='验证中';await p.run('refreshScreen()');assert.equal(p.nodes.get('apply-screen').disabled,true);assert.equal(p.nodes.get('screen-state').textContent,'验证中');
 p.screen.running=false;p.screen.confirmed=true;p.screen.phase='已更换';p.screen.canRestore=true;await p.run('refreshScreen()');
 assert.equal(p.nodes.get('screen-state').textContent,'已更换');assert.equal(p.nodes.get('restore-screen').disabled,false);assert.equal(p.nodes.get('set-lighting').disabled,false);
});

test('backend restart re-reads even when connection generation starts over',async()=>{
 const p=page();await settle();p.state.session='restarted';p.state.connectionGeneration=1;p.state.rgb.effect=4;
 await p.run('refresh()');await settle();
 assert.equal(p.nodes.get('lighting-effect').value,'static');assert.equal(p.calls.filter(c=>c.path==='/api/query').length,16);
});
test('interrupted screen job unlocks controls and reconnect re-reads configuration',async()=>{
 const p=page();await settle();p.screen.draftId='draft';await p.run("screenDraft='draft';buttons()");
 await p.nodes.get('apply-screen').emit('click');await settle();
 p.state.connected=false;p.screen.running=false;p.screen.confirmed=false;p.screen.phase='失败';p.screen.error='屏保上传期间连接已断开';p.screen.canRestore=true;
 await p.run('refreshScreen()');await p.run('refresh()');await settle();
 assert.match(p.nodes.get('screen-state').textContent,/断开/);assert.equal(p.nodes.get('restore-screen').disabled,true);
 p.state.connected=true;p.state.connectionGeneration=2;await p.run('refresh()');await settle();
 assert.equal(p.nodes.get('restore-screen').disabled,false);assert.equal(p.nodes.get('set-lighting').disabled,false);
 assert.equal(p.calls.filter(c=>c.path==='/api/query').length,16);
 assert.equal(p.calls.filter(c=>c.path==='/api/screensaver/apply').length,1,'interrupted upload must not automatically replay');
});

test('manual device refresh tracks physical settings without overwriting edits',async()=>{
 const p=page();await settle();
 assert.equal(p.nodes.get('lcd-row-1').value,'');assert.equal(p.nodes.get('set-lcd-layout').disabled,true);
 assert.equal(p.run('loaded.lcd'),true);
 p.nodes.get('cooling-rpm').value='2100';await p.nodes.get('cooling-rpm').emit('input');
 p.state.cooling={source:0,slot:4,rpm:3534};p.state.lcd=false;
 await p.nodes.get('refresh-device').emit('click');await settle();
 assert.match(p.nodes.get('cooling-current').textContent,/4 档/);assert.equal(p.nodes.get('cooling-rpm').value,'2100');
 assert.equal(p.run('loaded.lcd'),true);
 assert.equal(p.presets[3].classes.has('active'),true);
 for(const [i,value] of ['cpu-temperature','fan-rpm','time'].entries()){p.nodes.get('lcd-row-'+(i+1)).value=value;await p.nodes.get('lcd-row-'+(i+1)).emit('change');}
 assert.equal(p.nodes.get('set-lcd-layout').disabled,false);
 await p.nodes.get('set-lcd-layout').emit('click');await settle();assert.match(p.nodes.get('lcd-layout-state').textContent,/已发送/);
 p.state.connectionGeneration++;await p.run('refresh()');await settle();assert.equal(p.nodes.get('lcd-row-1').value,'');
});
test('slot click uses latest device configuration and duplicate clicks are blocked',async()=>{
 const p=page();await settle();p.state.presets[1]=2500;
 await p.presets[1].emit('click');await p.presets[2].emit('click');await settle();
 const writes=p.calls.filter(c=>c.path==='/api/cooling');assert.equal(writes.length,1);assert.deepEqual(writes[0].body,{source:0,slot:2,rpm:2500});
 p.state.connected=false;await p.run('refresh()');await p.presets[1].emit('click');assert.equal(p.calls.filter(c=>c.path==='/api/cooling').length,1);
});

test('editing slot loads its own RPM and rejects non-increasing speeds',async()=>{
 const p=page();await settle();p.nodes.get('cooling-slot').value='2';await p.nodes.get('cooling-slot').emit('change');await settle();
 assert.equal(p.nodes.get('cooling-rpm').value,2134);
 for(const invalid of ['1200','2912']){p.nodes.get('cooling-rpm').value=invalid;await p.nodes.get('cooling-rpm').emit('input');await p.nodes.get('save-cooling').emit('click');await settle();assert.equal(p.calls.filter(c=>c.path==='/api/cooling').length,0);}
 p.nodes.get('cooling-rpm').value='2300';await p.nodes.get('cooling-rpm').emit('input');await p.nodes.get('save-cooling').emit('click');await settle();
 assert.deepEqual(p.calls.find(c=>c.path==='/api/cooling').body,{source:0,slot:2,rpm:2300});
});
