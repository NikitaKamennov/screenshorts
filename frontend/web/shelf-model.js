"use strict";

// Pure shelf rules, shared by the desktop UI and Node's built-in test runner.
(() => {
  const collator = new Intl.Collator("ru", {numeric:true,sensitivity:"base"});
  const stop = new Set("это этот эта эти для как или при что чтобы уже так все его она они оно под над без был была быть есть ещё еще тоже только очень and the with this that from have into are for you your not but can www http https com png screenshot скриншот screenshorts".split(" "));
  const statusLabels={ready:"Готов",manual:"Исправлен",no_text:"Без текста",queued:"В очереди",running:"Обработка",error:"Ошибка",cancelled:"Остановлен",unavailable:"Недоступен",interrupted:"Прерван"};
  const status=item=>statusLabels[item.ocr?.status]||"Не запускался";
  function ocr(item) {
    if (!item.ocr || item.ocr.input_sha256 !== item.image_sha256) return "";
    return item.ocr.has_correction ? item.ocr.corrected_text || "" : item.ocr.raw_text || "";
  }
  function content(item) { return [item.note,ocr(item)].filter(Boolean).join(" · ").replace(/\s+/g," ").trim(); }
  function day(item) {
    const date=new Date(item.created_at);
    return `${date.getFullYear()}-${String(date.getMonth()+1).padStart(2,"0")}-${String(date.getDate()).padStart(2,"0")}`;
  }
  function size(bytes) {
    if (!Number.isFinite(bytes) || bytes < 0) return "—";
    if (bytes<1024) return `${bytes} Б`;
    if (bytes<1048576) return `${(bytes/1024).toLocaleString("ru-RU",{maximumFractionDigits:1})} КиБ`;
    if(bytes<1073741824)return `${(bytes/1048576).toLocaleString("ru-RU",{maximumFractionDigits:1})} МиБ`;
    return `${(bytes/1073741824).toLocaleString("ru-RU",{maximumFractionDigits:1})} ГиБ`;
  }
  function sort(items, key, descending) {
    const compare=(a,b)=>{
      switch(key){
        case "name":return collator.compare(a.image_file,b.image_file);
        case "size":return (a.image_bytes||0)-(b.image_bytes||0);
        case "resolution":return a.width*a.height-b.width*b.height||a.width-b.width||a.height-b.height;
        case "ocr":return collator.compare(status(a),status(b));
        case "content":return collator.compare(content(a),content(b));
        default:return Date.parse(a.created_at)-Date.parse(b.created_at);
      }
    };
    return items.slice().sort((a,b)=>(descending?-1:1)*compare(a,b)||collator.compare(a.id,b.id));
  }
  function tokens(text) {
    const words=text.toLowerCase().replace(/ё/g,"е").slice(0,8192).match(/[\p{L}\p{N}]{3,}/gu)||[];
    const result=new Set();
    for(const word of words){if(!stop.has(word)){result.add(word.length>7?word.slice(0,7):word);if(result.size===96)break;}}
    return [...result];
  }
  // Bounded inverted-index neighbours + TF-IDF cosine. No model, network or
  // all-pairs comparison. Yield regularly so search and cancellation stay usable.
  async function cluster(items, cancelled=()=>false) {
    const docs=[],frequency=new Map();
    for(let i=0;i<items.length;i++){
      const words=tokens(content(items[i]));docs.push(words);
      for(const word of words)frequency.set(word,(frequency.get(word)||0)+1);
      if(i%128===0){await new Promise(resolve=>setTimeout(resolve,0));if(cancelled())return null;}
    }
    const vectors=docs.map(words=>{
      const weights=new Map(words.map(word=>[word,Math.log(1+items.length/(1+frequency.get(word)))+1]));
      const norm=Math.hypot(...weights.values())||1;
      return new Map([...weights].map(([word,value])=>[word,value/norm]));
    });
    const parent=items.map((_,i)=>i),postings=new Map();
    const find=i=>{while(parent[i]!==i){parent[i]=parent[parent[i]];i=parent[i];}return i;};
    for(let i=0;i<docs.length;i++){
      const rare=docs[i].slice().sort((a,b)=>frequency.get(a)-frequency.get(b)).slice(0,16),candidates=new Set();
      for(const word of rare){for(const j of postings.get(word)||[]){candidates.add(j);if(candidates.size>=100)break;}if(candidates.size>=100)break;}
      for(const j of candidates){
        let dot=0,shared=0;
        for(const [word,weight] of vectors[i]){const other=vectors[j].get(word);if(other){dot+=weight*other;shared++;}}
        if(dot>=0.34&&(shared>=2||(docs[i].length===1&&docs[j].length===1))){parent[find(i)]=find(j);}
      }
      for(const word of rare){const list=postings.get(word)||[];list.unshift(i);if(list.length>48)list.pop();postings.set(word,list);}
      if(i%64===0){await new Promise(resolve=>setTimeout(resolve,0));if(cancelled())return null;}
    }
    const buckets=new Map();
    items.forEach((item,i)=>{const key=find(i);if(!buckets.has(key))buckets.set(key,[]);buckets.get(key).push(item);});
    const groups=[],singles=[],empty=[];
    for(const values of buckets.values()){
      if(values.length>1){const sorted=sort(values,"date",true);groups.push({key:`similar-${sorted[0].id}`,label:`Похожие: ${content(sorted[0]).slice(0,90)}`,items:sorted});}
      else (content(values[0])?singles:empty).push(values[0]);
    }
    groups.sort((a,b)=>collator.compare(a.label,b.label));
    if(singles.length)groups.push({key:"other",label:"Другие снимки с текстом",items:sort(singles,"content",false)});
    if(empty.length)groups.push({key:"empty",label:"Без OCR и комментария для сравнения",items:sort(empty,"date",true)});
    return groups;
  }
  function range(order,anchor,id) {
    const start=order.indexOf(anchor),end=order.indexOf(id);
    return start<0||end<0?[id]:order.slice(Math.min(start,end),Math.max(start,end)+1);
  }
  const api={ocr,content,day,size,sort,cluster,range,status};
  if(typeof module!=="undefined")module.exports=api;
  else window.ShelfModel=api;
})();
