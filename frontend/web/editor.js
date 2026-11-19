"use strict";

// Every operation uses original image coordinates. Screen preview and final
// export use the same renderer; undo stores operations, never disk snapshots.
class SkrinEditor {
  constructor(canvas, onChange) {
    this.canvas=canvas;this.onChange=onChange;this.image=null;
    this.tool="pencil";this.color="#e24c3a";this.width=4;
    this.operations=[];this.future=[];this.crop=null;this.gesture=null;
    this.renderQueued=false;this.changed=false;this.generation=0;this.locked=false;
    canvas.addEventListener("pointerdown",event=>this.pointerDown(event));
    canvas.addEventListener("pointermove",event=>this.pointerMove(event));
    canvas.addEventListener("pointerup",event=>this.pointerUp(event));
    canvas.addEventListener("pointercancel",()=>{this.gesture=null;this.render();});
    for(const button of document.querySelectorAll("[data-editor-tool]"))button.addEventListener("click",()=>{
      this.tool=button.dataset.editorTool;
      for(const candidate of document.querySelectorAll("[data-editor-tool]"))candidate.classList.toggle("is-active",candidate===button);
      canvas.style.cursor=this.tool==="crop"?"crosshair":"crosshair";
    });
    document.getElementById("editor-color").addEventListener("input",event=>{this.color=event.target.value;});
    document.getElementById("editor-width").addEventListener("input",event=>{this.width=Number(event.target.value)||4;});
    document.getElementById("editor-undo").addEventListener("click",()=>this.undo());
    document.getElementById("editor-redo").addEventListener("click",()=>this.redo());
    window.addEventListener("keydown",event=>{
      const dialog=document.getElementById("editor-dialog");
      if(!dialog.open || !dialog.contains(document.activeElement) || /^(INPUT|TEXTAREA)$/.test(document.activeElement?.tagName))return;
      if((event.ctrlKey||event.metaKey)&&event.key.toLowerCase()==="z"){event.preventDefault();event.shiftKey?this.redo():this.undo();}
      if((event.ctrlKey||event.metaKey)&&event.key.toLowerCase()==="y"){event.preventDefault();this.redo();}
    });
  }

  async load(dataURL) {
    this.reset();
	const generation=this.generation;
    if(!dataURL.startsWith("data:image/png;base64,"))throw new Error("Редактор принимает только подготовленный PNG.");
    const image=new Image();
    await new Promise((resolve,reject)=>{image.onload=resolve;image.onerror=()=>reject(new Error("Не удалось открыть PNG в редакторе."));image.src=dataURL;});
	if(generation!==this.generation){image.src="";return false;}
    if(image.naturalWidth*image.naturalHeight>16000000)throw new Error("Изображение превышает 16 млн пикселей.");
    this.image=image;this.crop={x:0,y:0,width:image.naturalWidth,height:image.naturalHeight};this.render();this.buttons();return true;
  }

  reset() {
	this.generation+=1;
    if(this.image)this.image.src="";
    this.image=null;this.operations=[];this.future=[];this.crop=null;this.gesture=null;this.changed=false;
    this.canvas.width=1;this.canvas.height=1;this.buttons();
  }

  point(event) {
    const box=this.canvas.getBoundingClientRect();
    return {x:this.crop.x+Math.max(0,Math.min(1,(event.clientX-box.left)/box.width))*this.crop.width,y:this.crop.y+Math.max(0,Math.min(1,(event.clientY-box.top)/box.height))*this.crop.height};
  }

  pointerDown(event) {
    if(!this.image || this.locked || event.button!==0)return;
    event.preventDefault();this.canvas.setPointerCapture(event.pointerId);
    const point=this.point(event);
    this.gesture={kind:this.tool,color:this.color,width:this.width,start:point,end:point,points:[point]};this.render();
  }

  pointerMove(event) {
    if(!this.gesture)return;
    this.gesture.end=this.point(event);
    if(this.gesture.kind==="pencil")this.gesture.points.push(this.gesture.end);
    this.requestRender();
  }

  pointerUp(event) {
    if(!this.gesture)return;
    this.gesture.end=this.point(event);
    const action=this.gesture;this.gesture=null;
    if(this.canvas.hasPointerCapture(event.pointerId))this.canvas.releasePointerCapture(event.pointerId);
    if(action.kind==="crop"){
      const box=this.box(action);
      if(box.width<1 || box.height<1){this.render();return;}
      action.previousCrop={...this.crop};action.nextCrop=box;this.crop={...box};
    }
    this.operations.push(action);this.future=[];this.commit();
  }

  commit(){this.changed=true;this.render();this.buttons();if(this.onChange)this.onChange();}
  undo(){if(this.locked||!this.operations.length)return;const action=this.operations.pop();if(action.kind==="crop")this.crop={...action.previousCrop};this.future.push(action);this.commit();}
  redo(){if(this.locked||!this.future.length)return;const action=this.future.pop();if(action.kind==="crop")this.crop={...action.nextCrop};this.operations.push(action);this.commit();}
  buttons(){document.getElementById("editor-undo").disabled=this.locked||!this.operations.length;document.getElementById("editor-redo").disabled=this.locked||!this.future.length;}

  canRemoveBackground(){
    const snapshots=this.operations.filter(action=>action.kind==="background");
    return this.image&&this.crop&&snapshots.length<4&&snapshots.reduce((total,action)=>total+action.width*action.height,0)+this.crop.width*this.crop.height<=64000000;
  }

  async applyBackground(dataURL,isCurrent=()=>true){
    if(!this.canRemoveBackground())throw new Error("История удаления фона заполнена. Отмени предыдущее удаление или сохрани снимок.");
    const generation=this.generation,crop={...this.crop},image=new Image();
    await new Promise((resolve,reject)=>{image.onload=resolve;image.onerror=()=>reject(new Error("Не удалось открыть результат удаления фона."));image.src=dataURL;});
    if(this.generation!==generation||!isCurrent())return false;
    if(image.naturalWidth!==crop.width||image.naturalHeight!==crop.height)throw new Error("Размер результата удаления фона изменился.");
    this.operations.push({kind:"background",image,x:crop.x,y:crop.y,width:crop.width,height:crop.height});
    this.future=[];this.commit();return true;
  }
  requestRender(){if(this.renderQueued)return;this.renderQueued=true;requestAnimationFrame(()=>{this.renderQueued=false;this.render();});}

  box(action){const x=Math.floor(Math.min(action.start.x,action.end.x)),y=Math.floor(Math.min(action.start.y,action.end.y));return {x,y,width:Math.max(0,Math.floor(Math.max(action.start.x,action.end.x))-x),height:Math.max(0,Math.floor(Math.max(action.start.y,action.end.y))-y)};}

  render(){
    if(!this.image || !this.crop)return;
    const scale=Math.min(1,1600/Math.max(this.crop.width,this.crop.height));
    this.paint(this.canvas,scale,true);
    document.getElementById("editor-dimensions").textContent=`${this.crop.width} × ${this.crop.height} px`;
  }

  paint(canvas,scale,preview){
    canvas.width=Math.max(1,Math.round(this.crop.width*scale));canvas.height=Math.max(1,Math.round(this.crop.height*scale));
    const context=canvas.getContext("2d");
    context.setTransform(scale,0,0,scale,-this.crop.x*scale,-this.crop.y*scale);
    context.drawImage(this.image,0,0);
    for(const action of this.operations)this.draw(context,action);
    if(preview&&this.gesture){
      if(this.gesture.kind==="crop"){
        const box=this.box(this.gesture);context.save();context.strokeStyle="#2d6a4e";context.lineWidth=2/scale;context.setLineDash([7/scale,5/scale]);context.strokeRect(box.x,box.y,box.width,box.height);context.restore();
      }else this.draw(context,this.gesture);
    }
  }

  draw(context,action){
    if(action.kind==="crop")return;
    if(action.kind==="background"){
      context.clearRect(action.x,action.y,action.width,action.height);
      context.drawImage(action.image,action.x,action.y,action.width,action.height);return;
    }
    context.save();context.lineCap="round";context.lineJoin="round";context.strokeStyle=action.color;context.fillStyle=action.color;context.lineWidth=action.width;
    const a=action.start,b=action.end,box=this.box(action);
    const line=(from,to)=>{context.beginPath();context.moveTo(from.x,from.y);context.lineTo(to.x,to.y);context.stroke();};
    const head=(from,to)=>{const angle=Math.atan2(to.y-from.y,to.x-from.x),length=Math.max(12,action.width*4);context.beginPath();context.moveTo(to.x,to.y);context.lineTo(to.x-length*Math.cos(angle-0.45),to.y-length*Math.sin(angle-0.45));context.lineTo(to.x-length*Math.cos(angle+0.45),to.y-length*Math.sin(angle+0.45));context.closePath();context.fill();};
    switch(action.kind){
      case "pencil":context.beginPath();context.moveTo(a.x,a.y);for(const point of action.points)context.lineTo(point.x,point.y);if(action.points.length===1)context.lineTo(a.x+0.01,a.y+0.01);context.stroke();break;
      case "line":line(a,b);break;
      case "arrow":line(a,b);head(a,b);break;
      case "double-arrow":line(a,b);head(a,b);head(b,a);break;
      case "elbow-arrow":{const turn={x:a.x,y:b.y};line(a,turn);line(turn,b);head(turn,b);break;}
      case "rect":context.strokeRect(box.x,box.y,box.width,box.height);break;
      case "ellipse":context.beginPath();context.ellipse(box.x+box.width/2,box.y+box.height/2,box.width/2,box.height/2,0,0,Math.PI*2);context.stroke();break;
      case "redact":context.fillStyle="#000000";context.fillRect(box.x,box.y,box.width,box.height);break;
    }
    context.restore();
  }

  exportDataURL(){
    if(!this.image||!this.crop)throw new Error("Изображение ещё не готово.");
    const canvas=document.createElement("canvas");
    this.paint(canvas,1,false);
    // Flattened pixels only: no original image, operations or history are sent.
    const data=canvas.toDataURL("image/png");canvas.width=1;canvas.height=1;
    return data;
  }
}

window.SkrinEditor=SkrinEditor;
