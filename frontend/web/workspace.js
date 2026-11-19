"use strict";

class SkrinWorkspace {
  constructor(onChange, isLocked, maximise) {
    this.dialog = document.getElementById("editor-dialog");
    this.body = document.getElementById("editor-body");
    this.split = document.getElementById("editor-splitter");
    this.corner = document.getElementById("editor-resize");
    this.maxButton = document.getElementById("editor-maximise");
    this.onChange = onChange; this.isLocked = isLocked; this.maximise = maximise;
    this.sizes = {panel_width:320, panel_height:280, width:1200, height:900};
    this.gesture = null; this.fill = false;
    for (const target of [this.split, this.corner]) {
      target.addEventListener("pointerdown", event => this.start(event, target));
      target.addEventListener("pointermove", event => this.move(event));
      target.addEventListener("pointerup", () => this.finish(false));
      target.addEventListener("pointercancel", () => this.finish(true));
      target.addEventListener("lostpointercapture", () => this.finish(true));
      target.addEventListener("keydown", event => this.key(event, target));
    }
    this.maxButton.addEventListener("click", async () => {
      if (this.isLocked() || this.gesture) return;
      const next = !this.fill;
      try { await this.maximise(next); this.fill = next; this.render(); }
      catch(error) { document.getElementById("editor-error").textContent = String(error); document.getElementById("editor-error").hidden = false; }
    });
    this.body.addEventListener("layoutchange", () => this.render());
    this.observer = new ResizeObserver(() => this.render());
    this.observer.observe(this.body);
    window.addEventListener("resize", () => this.render());
    window.addEventListener("keydown", event => {
      if (event.key === "Escape" && this.gesture) { event.preventDefault(); event.stopPropagation(); this.finish(true); }
    }, true);
    this.dialog.addEventListener("close", () => this.finish(true));
    this.render();
  }

  layout() { return {...this.sizes}; }
  horizontal() { return ["top", "bottom"].includes(this.body.dataset.panelPosition); }
  apply(layout) {
    for (const key of Object.keys(this.sizes)) if (Number.isFinite(layout[key]) && layout[key] > 0) this.sizes[key] = layout[key];
    this.render();
  }

  panelBounds() {
    const rect = this.body.getBoundingClientRect();
    const maximum = Math.max(80, (this.horizontal() ? rect.height : rect.width)-188);
    return {minimum:Math.min(this.horizontal()?140:240, maximum), maximum:Math.min(this.horizontal()?1200:1600, maximum)};
  }

  render() {
    const maxWidth = Math.max(300, Math.min(3840, window.innerWidth-32));
    const maxHeight = Math.max(300, Math.min(2160, window.innerHeight-32));
    this.dialog.style.width = `${this.fill ? maxWidth : Math.min(this.sizes.width, maxWidth)}px`;
    this.dialog.style.height = `${this.fill ? maxHeight : Math.min(this.sizes.height, maxHeight)}px`;
    const {minimum, maximum} = this.panelBounds();
    const size = Math.round(Math.max(minimum, Math.min(maximum, this.horizontal()?this.sizes.panel_height:this.sizes.panel_width)));
    this.body.style.setProperty("--panel-width", `${size}px`);
    this.body.style.setProperty("--panel-height", `${size}px`);
    this.split.setAttribute("aria-orientation", this.horizontal()?"horizontal":"vertical");
    this.split.setAttribute("aria-valuemin", String(Math.round(minimum)));
    this.split.setAttribute("aria-valuemax", String(Math.round(maximum)));
    this.split.setAttribute("aria-valuenow", String(size));
    this.maxButton.textContent = this.fill ? "Обычный размер" : "Развернуть";
    this.maxButton.setAttribute("aria-pressed", String(this.fill));
  }

  start(event, target) {
    if (event.button !== 0 || this.isLocked() || this.gesture) return;
    event.preventDefault(); target.focus({preventScroll:true});
    const base = this.effectiveSizes();
    this.gesture = {target, id:event.pointerId, x:event.clientX, y:event.clientY, original:{...this.sizes}, base, fill:this.fill};
    target.setPointerCapture(event.pointerId);
    this.dialog.classList.add("is-resizing");
  }

  effectiveSizes() {
    const rect = this.dialog.getBoundingClientRect();
    const sizes = {...this.sizes, width:rect.width, height:rect.height};
    sizes[this.horizontal()?"panel_height":"panel_width"] = Number(this.split.getAttribute("aria-valuenow"));
    return sizes;
  }

  change(target, base, dx, dy) {
    if (target === this.corner) {
      this.fill = false;
      this.sizes.width = Math.max(Math.min(680, window.innerWidth-32), Math.min(3840, window.innerWidth-32, base.width+dx*2));
      this.sizes.height = Math.max(Math.min(480, window.innerHeight-32), Math.min(2160, window.innerHeight-32, base.height+dy*2));
    } else {
      const position = this.body.dataset.panelPosition;
      const delta = {right:-dx, left:dx, top:dy, bottom:-dy}[position];
      const key = this.horizontal()?"panel_height":"panel_width";
      const {minimum, maximum} = this.panelBounds();
      this.sizes[key] = Math.round(Math.max(minimum, Math.min(maximum, base[key]+delta)));
    }
    this.render();
  }

  move(event) {
    const gesture = this.gesture;
    if (!gesture || gesture.id !== event.pointerId) return;
    if (this.isLocked()) { this.finish(true); return; }
    this.change(gesture.target, gesture.base, event.clientX-gesture.x, event.clientY-gesture.y);
  }

  finish(cancel) {
    const gesture = this.gesture; if (!gesture) return;
    this.gesture = null;
    if (gesture.target.hasPointerCapture(gesture.id)) gesture.target.releasePointerCapture(gesture.id);
    this.dialog.classList.remove("is-resizing");
    if (cancel) { this.sizes = gesture.original; this.fill = gesture.fill; this.render(); }
    else this.onChange();
  }

  key(event, target) {
    if (this.isLocked()) return;
    const step = event.shiftKey?40:10;
    const delta = {ArrowLeft:[-step,0], ArrowRight:[step,0], ArrowUp:[0,-step], ArrowDown:[0,step]}[event.key];
    if (!delta) return;
    event.preventDefault(); this.change(target, this.effectiveSizes(), ...delta); this.onChange();
  }
}

window.SkrinWorkspace = SkrinWorkspace;
