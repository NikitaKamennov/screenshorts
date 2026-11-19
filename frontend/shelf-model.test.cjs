const {test}=require('node:test');
const assert=require('node:assert/strict');
const M=require('./web/shelf-model.js');
const item=(id,note,more={})=>({id,image_file:`${id}.png`,note,created_at:'2026-11-17T10:00:00Z',width:100,height:200,image_bytes:100,image_sha256:'hash',...more});

test('content uses corrected OCR for the current pixels and preserves note',()=>{
  const row=item('a','мой текст',{ocr:{input_sha256:'hash',has_correction:true,corrected_text:'правка',raw_text:'obsolete'}});
  assert.equal(M.content(row),'мой текст · правка');
  row.ocr.input_sha256='old-hash';assert.equal(M.content(row),'мой текст');
  row.ocr.input_sha256='hash';row.ocr.corrected_text='';assert.equal(M.content(row),'мой текст');
});
test('all data columns sort numerically or by locale with deterministic ties',()=>{
  const rows=[item('10','z',{image_bytes:500,width:1000}),item('2','a',{image_bytes:200,width:50})];
  for(const key of ['name','size','resolution','content'])assert.deepEqual(M.sort(rows,key,false).map(row=>row.id),['2','10']);
  assert.deepEqual(M.sort(rows,'size',true).map(row=>row.id),['10','2']);
  assert.equal(M.size(1536),'1,5 КиБ');
});
test('similar text clusters while unrelated and missing OCR stay separate',async()=>{
  const rows=[item('a','оплата заказа интернет магазин доставка'),item('b','интернет магазин доставка заказа стоимость'),item('c','сборка программы golang компилятор'),item('d','')];
  const groups=await M.cluster(rows);
  assert(groups.some(group=>group.items.length===2&&group.items.map(item=>item.id).sort().join()==='a,b'));
  assert.equal(groups.find(group=>group.key==='empty').items[0].id,'d');
  assert.equal(await M.cluster(rows,()=>true),null);
});
test('shift selection follows sorted results, including non-rendered records',()=>{
  assert.deepEqual(M.range(['c','b','a','d'],'b','d'),['b','a','d']);
  assert.deepEqual(M.range(['a','b'],'missing','b'),['b']);
});
