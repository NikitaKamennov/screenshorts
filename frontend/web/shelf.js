"use strict";

(() => {
  const $=id=>document.getElementById(id), M=window.ShelfModel;
  const make=(tag,className,text)=>{const node=document.createElement(tag);if(className)node.className=className;if(text!==undefined)node.textContent=text;return node;};
  const defaults={view:"grid",sort:"date",descending:true,group_days:true,compact:false};
  class Shelf {
    constructor(options) {
      this.options=options;this.prefs={...defaults};this.items=[];this.all=[];this.order=[];this.chosen=new Set();this.limit=48;
      this.library="";this.query="";this.epoch=0;this.cache=new Map();this.imageQueue=[];this.imageBusy=false;this.busy=false;this.anchor=null;
      this.previewSerial=0;this.preferencesSync=Promise.resolve();this.sorting=false;
      this.attach();
    }
    bridge(){return this.options.bridge();}
    selection(ids=[...this.chosen]){return {library:this.library,ids};}
    async action(task) {
      if(this.busy||this.sorting)return;
      this.busy=true;this.hidePreview();this.updateSelection();
      try{return await task();}catch(error){
        const target=$("shelf-delete-dialog").open?$("shelf-delete-error"):$("shelf-trash-dialog").open?$("trash-error"):$("shelf-share-dialog").open?$("share-status"):null;
        if(target)target.textContent=String(error);else this.options.error(error);
      }finally{this.busy=false;this.updateSelection();if($("shelf-trash-dialog").open)this.trashSelection();}
    }
    setData(items,all,settings,query) {
      const switched=this.library!==settings.library_directory;
      if(switched){this.library=settings.library_directory;this.prefs={...defaults,...(settings.shelf?.view?settings.shelf:{})};this.chosen.clear();this.anchor=null;this.cache.clear();}
      if(switched||query!==this.query)this.limit=48;
      this.items=items;this.all=all;this.query=query;
      const available=new Set(items.map(item=>item.id));
      for(const id of this.chosen)if(!available.has(id))this.chosen.delete(id);
      void this.render();
    }
    change(value) {
      Object.assign(this.prefs,value);this.limit=48;void this.render();
      const payload={...this.prefs};
      this.preferencesSync=this.preferencesSync.catch(()=>{}).then(()=>this.bridge().SaveShelfPreferences(payload)).catch(error=>this.options.error(`Вид изменён, но не сохранён: ${String(error)}`));
    }
    controls() {
      $("view-grid").setAttribute("aria-pressed",String(this.prefs.view==="grid"));
      $("view-table").setAttribute("aria-pressed",String(this.prefs.view==="table"));
      $("shelf-sort").value=this.prefs.sort;
      $("shelf-direction").textContent=this.prefs.descending?"↓ По убыванию":"↑ По возрастанию";
      $("shelf-days").checked=this.prefs.group_days;$("shelf-days").disabled=this.prefs.sort!=="date";
      $("shelf-compact").checked=this.prefs.compact;
      for(const header of document.querySelectorAll("#shelf-table th[data-sort]"))header.setAttribute("aria-sort",header.dataset.sort===this.prefs.sort?(this.prefs.descending?"descending":"ascending"):"none");
    }
    async render() {
      const epoch=++this.epoch;this.hidePreview();this.controls();
      this.observer?.disconnect();for(const job of this.imageQueue.splice(0))job.reject(new Error("Обновление полки"));
      this.sorting=true;this.updateSelection();
      let groups=[];
      if(this.prefs.sort==="content") {
        $("shelf-status").textContent="Сопоставляем OCR и комментарии локально…";
        groups=await M.cluster(this.items,()=>epoch!==this.epoch);
        if(!groups||epoch!==this.epoch)return;
        if(this.prefs.descending)groups.reverse();
      }else{
        const items=M.sort(this.items,this.prefs.sort,this.prefs.descending);
        if(this.prefs.sort==="date"&&this.prefs.group_days){
          for(const item of items){const key=M.day(item);let group=groups[groups.length-1];if(group?.key!==key){group={key,label:new Date(item.created_at).toLocaleDateString("ru-RU",{weekday:"long",day:"numeric",month:"long",year:"numeric"}),items:[]};groups.push(group);}group.items.push(item);}
        }else groups=[{key:"all",label:"",items}];
      }
      if(epoch!==this.epoch)return;
      this.sorting=false;this.order=groups.flatMap(group=>group.items.map(item=>item.id));
      const table=this.prefs.view==="table";
      $("gallery").replaceChildren();$("shelf-rows").replaceChildren();
      $("gallery").hidden=table;$("shelf-table-wrap").hidden=!table||!this.items.length;
      $("gallery").classList.toggle("is-compact",this.prefs.compact);$("shelf-table").classList.toggle("is-compact",this.prefs.compact);
      $("empty-state").hidden=this.items.length>0;
      $("empty-title").textContent=this.query?"Пока ничего не нашлось":"Твоя полка начинается с одного скрина";
      $("empty-description").textContent=this.query?"Попробуй слово из OCR или комментария.":"Захвати область или импортируй PNG. Все сохранённые скриншоты будут здесь.";
      $("empty-import-button").hidden=Boolean(this.query);$("clear-search-button").hidden=!this.query;
      $("result-count").textContent=`${this.query?"Найдено":"На полке"}: ${this.items.length}`;
      $("load-more-button").hidden=this.items.length<=this.limit;
      $("load-more-button").textContent=`Показать ещё (${Math.min(48,Math.max(0,this.items.length-this.limit))})`;
      $("shelf-status").textContent=this.prefs.sort==="content"?"Похожие слова в OCR и комментариях → группы. Без визуального анализа; совпадение темы приблизительное.":"Чекбоксы — выбор, Shift — диапазон, Ctrl+A — все результаты, Escape — снять выбор.";
      const fragment=document.createDocumentFragment();let count=0;
      for(const group of groups){
        if(count>=this.limit)break;
        if(group.label){
          if(table){const row=make("tr","table-group"),cell=make("td","",group.label);cell.colSpan=8;cell.append(make("small","",`${group.items.length} снимков`));row.append(cell);fragment.append(row);}
          else{const heading=make("div","shelf-group",group.label);heading.append(make("small","",`${group.items.length} снимков`));fragment.append(heading);}
        }
        for(const item of group.items){if(count++>=this.limit)break;fragment.append(table?this.row(item):this.card(item));}
      }
      (table?$("shelf-rows"):$("gallery")).append(fragment);
      if(!table){
        this.observer=new IntersectionObserver(entries=>{for(const entry of entries)if(entry.isIntersecting){this.observer.unobserve(entry.target);this.loadCard(entry.target,epoch);}},{rootMargin:"150px"});
        for(const card of $("gallery").querySelectorAll(".screenshot-card"))this.observer.observe(card);
      }
      this.updateSelection();
    }
    check(item) {
      const box=make("input");box.type="checkbox";box.checked=this.chosen.has(item.id);box.dataset.select=item.id;
      box.setAttribute("aria-label",`Выделить ${item.image_file}`);
      box.addEventListener("click",event=>{event.stopPropagation();this.choose(item.id,box.checked,event.shiftKey);});
      return box;
    }
    card(item) {
      const card=$("screenshot-template").content.firstElementChild.cloneNode(true);card.dataset.screenshotId=item.id;
      card.querySelector(".card-select").replaceChildren(this.check(item));
      card.querySelector(".card-title").textContent=item.image_file;
      card.querySelector(".card-note").textContent=M.content(item)||this.options.ocrLabel(item.ocr);
      card.querySelector(".card-date").textContent=this.options.date(item.created_at);
      card.querySelector(".card-facts").textContent=`${M.size(item.image_bytes)} · ${item.width} × ${item.height}`;
      card.querySelector(".card-dimensions").textContent=`${item.width} × ${item.height}`;
      const open=card.querySelector(".card-open");open.setAttribute("aria-label",`Открыть ${item.image_file}`);
      open.addEventListener("click",event=>{if(event.ctrlKey||event.metaKey||event.shiftKey)this.choose(item.id,!this.chosen.has(item.id),event.shiftKey);else this.options.open(item);});
      const remove=card.querySelector(".card-delete");remove.setAttribute("aria-label",`Удалить ${item.image_file}`);remove.addEventListener("click",()=>this.askDelete([item.id]));
      return card;
    }
    row(item) {
      const row=make("tr","shelf-row");row.dataset.screenshotId=item.id;
      const check=make("td","check-cell");check.append(this.check(item));row.append(check);
      const name=make("td"),open=make("button","table-name",item.image_file);open.title=item.image_file;open.addEventListener("click",()=>{this.hidePreview();this.options.open(item);});name.append(open);row.append(name);
      row.append(make("td","table-date",this.options.date(item.created_at)));
      const content=make("td","table-content");content.append(make("span","",M.content(item)||this.options.ocrLabel(item.ocr)));row.append(content);
      const size=make("td","table-size",M.size(item.image_bytes));size.title=`${(item.image_bytes||0).toLocaleString("ru-RU")} байт (PNG)`;row.append(size);
      row.append(make("td","table-resolution",`${item.width} × ${item.height}`));
      row.append(make("td","",M.status(item)));
      const actions=make("td"),remove=$("screenshot-template").content.querySelector(".card-delete").cloneNode(true);remove.className="text-button shelf-danger";remove.title="Переместить в корзину";remove.setAttribute("aria-label",`Удалить ${item.image_file}`);remove.addEventListener("click",()=>this.askDelete([item.id]));actions.append(remove);row.append(actions);
      row.addEventListener("click",event=>{if(event.target.closest("button,input,label"))return;if(!event.ctrlKey&&!event.metaKey&&!event.shiftKey)this.chosen.clear();this.choose(item.id,!this.chosen.has(item.id),event.shiftKey);});
      row.addEventListener("dblclick",event=>{if(!event.target.closest("button,input")){this.hidePreview();this.options.open(item);}});
      row.addEventListener("pointerenter",event=>{if(event.pointerType!=="touch")this.preview(item,event);});
      row.addEventListener("pointermove",event=>{this.pointer={x:event.clientX,y:event.clientY};this.positionPreview();});
      row.addEventListener("pointerleave",()=>this.hidePreview());
      return row;
    }
    choose(id,checked,shift) {
      if(this.busy||this.sorting){this.updateSelection();return;}
      const ids=shift?M.range(this.order,this.anchor,id):[id];
      for(const key of ids)checked?this.chosen.add(key):this.chosen.delete(key);
      this.anchor=id;this.updateSelection();
    }
    updateSelection() {
      const count=this.chosen.size,total=this.items.length;
      $("shelf-selection-count").textContent=count?`Выбрано ${count} из ${total} · ${M.size(this.items.filter(item=>this.chosen.has(item.id)).reduce((sum,item)=>sum+(item.image_bytes||0),0))}`:"Ничего не выделено";
      for(const id of ["shelf-select-all","table-select-all"]){const box=$(id);box.checked=total>0&&count===total;box.indeterminate=count>0&&count<total;box.disabled=!total||this.busy||this.sorting;}
      for(const box of document.querySelectorAll("[data-select]")){box.checked=this.chosen.has(box.dataset.select);box.disabled=this.busy||this.sorting;box.closest("[data-screenshot-id]")?.classList.toggle("is-selected",box.checked);}
      for(const button of document.querySelectorAll("[data-shelf-action]"))button.disabled=!count||this.busy||this.sorting;
      for(const button of document.querySelectorAll(".card-delete,.shelf-row .shelf-danger"))button.disabled=this.busy||this.sorting;
      $("shelf-clear").disabled=!count||this.busy;$("load-more-button").disabled=this.sorting;
    }
    async image(item) {
      const key=`${this.library}|${item.id}|${item.image_sha256}`;
      if(this.cache.has(key))return this.cache.get(key);
      return new Promise((resolve,reject)=>{this.imageQueue.push({item,key,library:this.library,resolve,reject});void this.pumpImages();});
    }
    async pumpImages() {
      if(this.imageBusy)return;this.imageBusy=true;
      try{while(this.imageQueue.length){
        const job=this.imageQueue.shift();
        try{if(job.library!==this.library)throw new Error("Библиотека сменилась");const image=await this.bridge().GetShelfThumbnail(job.library,job.item.id);if(typeof image!=="string"||!image.startsWith("data:image/png;base64,"))throw new Error("Нет предпросмотра");this.cache.set(job.key,image);while(this.cache.size>32)this.cache.delete(this.cache.keys().next().value);job.resolve(image);}catch(error){job.reject(error);}
      }}finally{this.imageBusy=false;}
    }
    async loadCard(card,epoch) {
      const item=this.items.find(value=>value.id===card.dataset.screenshotId);if(!item)return;
      try{const url=await this.image(item);if(epoch!==this.epoch||!card.isConnected)return;const img=card.querySelector("img");img.src=url;img.hidden=false;card.querySelector(".thumbnail-status").hidden=true;}
      catch(error){if(card.isConnected){const status=card.querySelector(".thumbnail-status");status.textContent="Нет предпросмотра";status.title=String(error);}}
    }
    preview(item,event) {
      this.hidePreview();this.pointer={x:event.clientX,y:event.clientY};const serial=this.previewSerial;
      this.previewTimer=setTimeout(async()=>{
        if(serial!==this.previewSerial||document.querySelector("dialog[open]"))return;
        const tooltip=$("shelf-preview"),img=tooltip.querySelector("img");img.hidden=true;img.removeAttribute("src");
        tooltip.querySelector("p").textContent=M.content(item).slice(0,500)||this.options.ocrLabel(item.ocr);
        tooltip.querySelector("small").textContent=`${item.width} × ${item.height} · ${M.size(item.image_bytes)} · Загрузка…`;
        tooltip.hidden=false;this.positionPreview();
        try{const url=await this.image(item);if(serial!==this.previewSerial)return;img.src=url;img.hidden=false;tooltip.querySelector("small").textContent=`${item.width} × ${item.height} · ${M.size(item.image_bytes)}`;this.positionPreview();}
        catch(error){if(serial===this.previewSerial)tooltip.querySelector("small").textContent="Предпросмотр недоступен";}
      },220);
    }
    positionPreview() {
      const tooltip=$("shelf-preview");if(tooltip.hidden||!this.pointer)return;
      const rect=tooltip.getBoundingClientRect(),gap=18;
      let x=this.pointer.x+gap,y=this.pointer.y+gap;
      if(x+rect.width>innerWidth-12)x=this.pointer.x-rect.width-gap;
      if(y+rect.height>innerHeight-12)y=this.pointer.y-rect.height-gap;
      tooltip.style.left=`${Math.max(12,Math.min(x,innerWidth-rect.width-12))}px`;tooltip.style.top=`${Math.max(12,Math.min(y,innerHeight-rect.height-12))}px`;
    }
    hidePreview(){clearTimeout(this.previewTimer);this.previewSerial++;$("shelf-preview").hidden=true;}
    askDelete(ids=[...this.chosen]) {
      if(this.busy||this.sorting||!ids.length)return;
      this.hidePreview();this.deleteSelection=this.selection(ids);
      $("shelf-delete-description").textContent=ids.length===1?`«${this.items.find(item=>item.id===ids[0])?.image_file||ids[0]}» будет убран с полки.`:`Выбрано снимков: ${ids.length}. Все они будут убраны с полки.`;
      $("shelf-delete-error").textContent="";$("shelf-delete-dialog").showModal();
    }
    async confirmDelete() {
      const value=this.deleteSelection;
      await this.action(async()=>{
        $("shelf-delete-confirm").disabled=true;
        try{const result=await this.bridge().TrashScreenshots(value);
          for(const id of result.done||[])this.chosen.delete(id);
          $("shelf-delete-dialog").close();await this.options.refresh();
          this.options.toast(`В корзине: ${result.done?.length||0}. Восстановление — кнопка «Корзина».`);
          if(result.errors?.length)this.options.error(result.errors.join("\n"));
        }finally{$("shelf-delete-confirm").disabled=false;}
      });
    }
    async batch(kind) {
      if(kind==="delete"){this.askDelete();return;}
      if(kind==="share"){this.shareSelection=this.selection();this.hidePreview();$("share-status").textContent=`Выбрано: ${this.chosen.size}`;$("shelf-share-dialog").showModal();return;}
      const value=this.selection();
      await this.action(async()=>{
        if(kind==="copy"){await this.bridge().CopyShelfFiles(value,false);this.options.toast("Файлы скопированы. Вставь их через Ctrl+V.");}
        if(kind==="text"){await this.bridge().CopyShelfText(value);this.options.toast("OCR и комментарии скопированы.");}
        if(kind==="zip"&&await this.bridge().ExportShelfZIP(value))this.options.toast("ZIP с PNG и метаданными сохранён.");
      });
    }
    async openTrash() {
      this.hidePreview();this.trashLibrary=this.library;this.trashEntries=[];$("shelf-trash-list").textContent="Загрузка…";$("trash-error").textContent="";$("trash-select-all").checked=false;$("trash-restore").disabled=true;$("trash-count").textContent="";$("shelf-trash-dialog").showModal();
      try{this.trashEntries=await this.bridge().ListTrash();this.renderTrash();}catch(error){$("trash-error").textContent=String(error);}
    }
    renderTrash() {
      const fragment=document.createDocumentFragment();
      for(const entry of this.trashEntries){const row=make("div","trash-entry"),label=make("label"),box=make("input"),text=make("span","",entry.record.image_file);box.type="checkbox";box.dataset.trashToken=entry.token;box.addEventListener("change",()=>this.trashSelection());text.append(make("small","",`${this.options.date(entry.deleted_at)} · ${M.size(entry.record.image_bytes)} · ${entry.record.width} × ${entry.record.height}`));label.append(box,text);row.append(label);fragment.append(row);}
      $("shelf-trash-list").replaceChildren(fragment);
      if(!this.trashEntries.length)$("shelf-trash-list").textContent="Корзина пуста.";
      this.trashSelection();
    }
    trashSelection() {
      const boxes=[...document.querySelectorAll("[data-trash-token]")],count=boxes.filter(box=>box.checked).length;
      $("trash-select-all").checked=!!boxes.length&&count===boxes.length;$("trash-select-all").indeterminate=count>0&&count<boxes.length;
      $("trash-count").textContent=`Выбрано: ${count} из ${boxes.length}`;$("trash-restore").disabled=this.busy||!count;
    }
    attach() {
      for(const view of ["grid","table"])$("view-"+view).addEventListener("click",()=>this.change({view}));
      $("shelf-sort").addEventListener("change",()=>this.change({sort:$("shelf-sort").value,descending:$("shelf-sort").value==="date"}));
      $("shelf-direction").addEventListener("click",()=>this.change({descending:!this.prefs.descending}));
      $("shelf-days").addEventListener("change",()=>this.change({group_days:$("shelf-days").checked}));
      $("shelf-compact").addEventListener("change",()=>this.change({compact:$("shelf-compact").checked}));
      for(const head of document.querySelectorAll("th[data-sort]"))head.querySelector("button").addEventListener("click",()=>this.change({sort:head.dataset.sort,descending:this.prefs.sort===head.dataset.sort?!this.prefs.descending:head.dataset.sort==="date"}));
      for(const id of ["shelf-select-all","table-select-all"])$(id).addEventListener("change",event=>{this.chosen=event.target.checked?new Set(this.order):new Set();this.updateSelection();});
      $("shelf-clear").addEventListener("click",()=>{this.chosen.clear();this.updateSelection();});
      $("load-more-button").addEventListener("click",()=>{this.limit+=48;void this.render();});
      for(const button of document.querySelectorAll("[data-shelf-action]"))button.addEventListener("click",()=>void this.batch(button.dataset.shelfAction));
      $("shelf-delete-confirm").addEventListener("click",()=>void this.confirmDelete());
      $("shelf-folder").addEventListener("click",()=>void this.action(()=>this.bridge().OpenShelfFolder()));
      $("shelf-refresh").addEventListener("click",()=>void this.action(()=>this.options.refresh()));
      $("shelf-trash").addEventListener("click",()=>{if(!this.busy)void this.openTrash();});
      $("trash-select-all").addEventListener("change",event=>{for(const box of document.querySelectorAll("[data-trash-token]"))box.checked=event.target.checked;this.trashSelection();});
      $("trash-restore").addEventListener("click",()=>void this.action(async()=>{
        $("trash-restore").disabled=true;
        const tokens=[...document.querySelectorAll("[data-trash-token]:checked")].map(box=>box.dataset.trashToken);
        try{const result=await this.bridge().RestoreTrash(this.trashLibrary,tokens);$("trash-error").textContent=(result.errors||[]).join("\n");this.trashEntries=this.trashEntries.filter(entry=>!result.done?.includes(entry.token));this.renderTrash();await this.options.refresh();this.options.toast(`Восстановлено: ${result.done?.length||0}`);}finally{this.trashSelection();}
      }));
      $("share-copy").addEventListener("click",()=>void this.action(async()=>{await this.bridge().CopyShelfFiles(this.shareSelection,$("share-metadata").checked);$("share-status").textContent="Файлы в буфере. Вставь их в выбранное приложение через Ctrl+V.";}));
      $("share-zip").addEventListener("click",()=>void this.action(async()=>{if(await this.bridge().ExportShelfZIP(this.shareSelection))$("share-status").textContent="ZIP сохранён. Прикрепи его к сообщению или письму.";}));
      for(const id of ["shelf-delete-dialog","shelf-trash-dialog","shelf-share-dialog"]){$(id).addEventListener("cancel",event=>{if(this.busy)event.preventDefault();});$(id).addEventListener("click",event=>{if(this.busy&&event.target.closest("[data-close-dialog]"))event.stopImmediatePropagation();},true);}
      window.addEventListener("scroll",()=>this.hidePreview(),true);window.addEventListener("blur",()=>this.hidePreview());
      window.addEventListener("keydown",event=>{
        if(event.key==="F5"&&!document.querySelector("dialog[open]")&&!this.busy){event.preventDefault();void this.action(()=>this.options.refresh());return;}
        if(event.defaultPrevented||event.target.isContentEditable||event.target.closest("input:not([type=checkbox]),textarea,select")||document.querySelector("dialog[open]")||this.busy||this.sorting)return;
        if((event.ctrlKey||event.metaKey)&&event.key.toLowerCase()==="a"){event.preventDefault();this.chosen=new Set(this.order);this.updateSelection();}
        if(event.key==="Escape"){this.chosen.clear();this.updateSelection();this.hidePreview();}
        if(event.key==="Delete"&&this.chosen.size){event.preventDefault();this.askDelete();}
        if((event.ctrlKey||event.metaKey)&&event.key.toLowerCase()==="c"&&this.chosen.size&&!String(window.getSelection())){event.preventDefault();void this.batch("copy");}
      });
    }
  }
  window.ScreenShortsShelf=Shelf;
})();
