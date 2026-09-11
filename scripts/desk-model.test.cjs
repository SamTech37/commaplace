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
