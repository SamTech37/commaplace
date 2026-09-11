const {test}=require('node:test');
const assert=require('node:assert/strict');
const M=require('../internal/handlers/static/desk-model.js');
test('resizing is zero sum and never overlaps at either bound',()=>{
 const panes=[{kind:'edit',ref:'a',width:480},{kind:'feed',width:340},{kind:'saved',width:340}];
 for(let i=0;i<500;i++){
  M.resize(panes,i%2,(i%7-3)*193);
  assert.equal(panes.reduce((n,p)=>n+p.width,0),1160);
  assert.ok(panes.every(p=>p.width>=260&&p.width<=1200));
  const rects=M.positions(panes);for(let j=1;j<rects.length;j++)assert.equal(rects[j].x-rects[j-1].x-rects[j-1].width,18);
 }
});
test('reordering preserves content identity, sizes and reading positions',()=>{
 const a={kind:'note',ref:'a',width:430,scroll:132},b={kind:'edit',ref:'b',width:480,scroll:41};const panes=[a,b];M.move(panes,0,1);assert.equal(panes[1],a);assert.equal(panes[0],b);assert.equal(a.scroll,132);M.move(panes,0,-1);assert.deepEqual(panes,[b,a]);
});
test('snapshots exclude article content and computed URLs',()=>{
 const snapshot=M.snapshot({windows:[{kind:'edit',ref:'a',width:480,body:'private text',title:'Private title',url:'/alice/title'}],active:'edit:a:',scrollLeft:40});assert.equal(snapshot.windows[0].body,undefined);assert.equal(snapshot.windows[0].url,undefined);assert.equal(snapshot.windows[0].title,undefined);assert.equal(snapshot.windows[0].key,'edit:a:');
});
test('only same-origin HTTP URLs are eligible for desk navigation',()=>{
 const origin='https://commaplace.app';assert.equal(M.safeURL('/alice/文章?x=1',origin),'/alice/%E6%96%87%E7%AB%A0?x=1');for(const raw of ['javascript:alert(1)','data:text/html,x','https://evil.example/x','//evil.example/x','https://user:pass@commaplace.app/x'])assert.equal(M.safeURL(raw,origin),null);
});
test('viewport fits available space without overlaps at desktop, tablet and phone sizes',()=>{
 for(const width of [240,320,390,585,985,1440,1920,2560]) for(const count of [0,1,2,3,8,64]) {
  const panes=Array.from({length:count},(_,i)=>({kind:'note',ref:String(i),width:430}));
  const rects=M.viewport(panes,width);
  assert.equal(rects.length,count);
  rects.forEach((r,i)=>{assert.ok(r.width>0&&r.width<=width-28);if(i)assert.ok(Math.abs(r.x-rects[i-1].x-rects[i-1].width-M.gap)<.01);});
  if(count&&count*260+(count-1)*18<=width-28&&count*1200+(count-1)*18>=width-28)assert.ok(Math.abs(rects.at(-1).x+rects.at(-1).width+14-width)<.01);
  assert.ok(panes.every(p=>p.width===430),'resizing screen must not overwrite saved widths');
 }
});
test('resizing fitted panes preserves the total and exact pointer delta',()=>{
 const panes=[{kind:'note',ref:'a',width:430},{kind:'note',ref:'b',width:430}];
 M.viewport(panes,1000).forEach((r,i)=>panes[i].width=r.width);
 const before=M.viewport(panes,1000);M.resize(panes,0,70);const after=M.viewport(panes,1000);
 assert.equal(after[0].width-before[0].width,70);assert.equal(before[1].width-after[1].width,70);
});
test('reader retention is bounded while visible panes and loaded editors stay alive',()=>{
 const windows=Array.from({length:20},(_,i)=>({kind:'note',ref:String(i)}));
 windows.push({kind:'edit',ref:'draft'},{kind:'edit',ref:'unloaded'});
 const loaded=new Map(windows.slice(0,-1).map((p,i)=>[M.key(p),i]));
 const visible=new Set([M.key(windows[0]),M.key(windows[1])]);
 const keep=M.retainedKeys(windows,visible,loaded);
 assert.equal(keep.size,5);assert.ok(keep.has('edit:draft:'));assert.ok(!keep.has('edit:unloaded:'));
 assert.ok([...visible].every(k=>keep.has(k)));assert.ok(keep.has('note:19:'));assert.ok(keep.has('note:18:'));
});
