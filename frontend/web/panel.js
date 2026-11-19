"use strict";

// Moving the original DOM nodes preserves text, focus and all tool listeners.
// Pointer events work independently of native file drag-and-drop in the webview.
class SkrinToolPanel {
  constructor(onChange, isLocked) {
    this.root = document.getElementById("editor-tools");
    this.body = document.getElementById("editor-body");
    this.blocks = document.getElementById("editor-tool-blocks");
    this.button = document.getElementById("editor-panel-position");
    this.status = document.getElementById("editor-layout-status");
    this.onChange = onChange;
    this.isLocked = isLocked;
    this.positions = ["right", "bottom", "left", "top"];
    this.labels = {right:"справа", bottom:"снизу", left:"слева", top:"сверху"};
    this.drag = null;
    this.expanded = null;
    this.lastClick = null;
    this.button.addEventListener("click", () => {
      if (this.isLocked() || this.drag) return;
      const index = this.positions.indexOf(this.body.dataset.panelPosition);
      this.setPosition(this.positions[(index+1)%this.positions.length]);
      this.changed();
    });
    this.root.addEventListener("pointerdown", event => this.pointerDown(event));
    this.root.addEventListener("pointermove", event => this.pointerMove(event));
    this.root.addEventListener("pointerup", event => {
      if (this.drag?.pointerID === event.pointerId) this.finishDrag(false);
    });
    this.root.addEventListener("pointercancel", () => this.finishDrag(true));
    this.root.addEventListener("lostpointercapture", () => this.finishDrag(true));
    this.root.addEventListener("keydown", event => this.keyDown(event));
    window.addEventListener("keydown", event => {
      if (event.key === "Escape" && this.drag) {
        event.preventDefault();
        event.stopPropagation();
        this.finishDrag(true);
      } else if (event.key === "Escape" && this.expanded && document.getElementById("editor-dialog").open && !document.getElementById("editor-dialog").classList.contains("is-resizing")) {
        event.preventDefault(); event.stopPropagation(); this.setExpanded(null);
      }
    }, true);
    document.getElementById("editor-dialog").addEventListener("close", () => this.finishDrag(true));
    this.setPosition("right");
    this.updateLabels();
  }

  items() { return [...this.blocks.children].filter(item => item.dataset.toolBlock); }
  layout() { return {position:this.body.dataset.panelPosition, tool_order:this.items().map(item => item.dataset.toolBlock)}; }
  horizontal() { return ["top", "bottom"].includes(this.body.dataset.panelPosition); }

  focusFirst() {
    this.root.scrollTop = 0;
    this.blocks.scrollLeft = 0;
    const first = this.items()[0];
    if (!first) return;
    first.scrollTop = 0;
    const target = first.querySelector('textarea:not([readonly]), button:not([data-tool-handle]):not(:disabled), input:not(:disabled)') || first.querySelector("[data-tool-handle]");
    target?.focus({preventScroll:true});
  }

  apply(layout) {
    this.finishDrag(true);
    this.setPosition(this.positions.includes(layout?.position) ? layout.position : "right");
    const nodes = new Map(this.items().map(item => [item.dataset.toolBlock, item]));
    const order = Array.isArray(layout?.tool_order) ? layout.tool_order : [];
    for (const id of [...new Set([...order, ...nodes.keys()])]) {
      if (nodes.has(id)) this.blocks.append(nodes.get(id));
    }
    this.updateLabels();
  }

  setPosition(position) {
    this.body.dataset.panelPosition = position;
    this.button.textContent = `Панель: ${this.labels[position]}`;
    const next = this.positions[(this.positions.indexOf(position)+1)%this.positions.length];
    this.button.title = `Положение панели: ${this.labels[position]}. Нажми, чтобы разместить ${this.labels[next]}.`;
    this.button.setAttribute("aria-label", this.button.title);
    this.body.dispatchEvent(new Event("layoutchange"));
  }

  setExpanded(id) {
    this.expanded = id;
    this.blocks.classList.toggle("has-expanded", Boolean(id));
    this.root.classList.toggle("has-expanded", Boolean(id));
    for (const item of this.items()) {
      const active = item.dataset.toolBlock === id;
      item.classList.toggle("is-expanded", active);
      item.classList.toggle("is-collapsed", Boolean(id) && !active);
      item.querySelector("[data-tool-handle]").setAttribute("aria-expanded", String(!id || active));
    }
    this.status.textContent = id ? "Блок развёрнут. Повторный двойной щелчок или Escape вернёт остальные блоки." : "Все блоки развёрнуты.";
  }

  toggleExpanded(block) {
    const id = block.dataset.toolBlock;
    this.setExpanded(this.expanded === id ? null : id);
    block.scrollIntoView({block:"nearest", inline:"nearest"});
  }

  updateLabels() {
    const items = this.items();
    items.forEach((item, index) => {
      const handle = item.querySelector("[data-tool-handle]");
      const name = handle.querySelector("[data-block-name]")?.textContent || handle.textContent.replace("⠿", "").trim();
      handle.setAttribute("aria-label", `${name}. Блок ${index+1} из ${items.length}. Двойной щелчок или Enter разворачивает блок. Перетащи мышью или используй Alt+стрелки.`);
      handle.title = "Двойной щелчок — развернуть/вернуть; перетаскивание — порядок; Alt+стрелки — порядок с клавиатуры";
      handle.setAttribute("aria-expanded", String(!this.expanded || this.expanded === item.dataset.toolBlock));
    });
  }

  changed() {
    this.updateLabels();
    this.status.textContent = `Панель ${this.labels[this.body.dataset.panelPosition]}. Раскладка изменена.`;
    this.onChange(this.layout());
  }

  pointerDown(event) {
    const handle = event.target.closest("[data-tool-handle]");
    if (!handle || event.button !== 0 || this.isLocked() || this.drag) return;
    event.preventDefault();
    handle.focus({preventScroll:true});
    this.drag = {
      pointerID:event.pointerId, block:handle.closest("[data-tool-block]"),
      startX:event.clientX, startY:event.clientY, x:event.clientX, y:event.clientY,
      original:this.items(), moving:false, ghost:null, frame:0,
    };
    // The stable parent retains pointer capture when a block changes its place.
    this.root.setPointerCapture(event.pointerId);
  }

  pointerMove(event) {
    const drag = this.drag;
    if (!drag || drag.pointerID !== event.pointerId) return;
    drag.x = event.clientX;
    drag.y = event.clientY;
    if (!drag.moving && Math.hypot(drag.x-drag.startX, drag.y-drag.startY) >= 6) {
      this.lastClick = null;
      this.setExpanded(null);
      drag.moving = true;
      drag.block.classList.add("is-dragging");
      this.root.classList.add("is-reordering");
      drag.ghost = document.createElement("div");
      drag.ghost.className = "panel-drag-preview";
      drag.ghost.setAttribute("aria-hidden", "true");
      drag.ghost.textContent = drag.block.querySelector("[data-tool-handle]").textContent.trim();
      document.getElementById("editor-dialog").append(drag.ghost);
      this.dragFrame();
    }
    if (drag.moving) event.preventDefault();
  }

  dragFrame() {
    const drag = this.drag;
    if (!drag?.moving) return;
    if (this.isLocked()) { this.finishDrag(true); return; }
    drag.ghost.style.left = `${Math.max(8, Math.min(drag.x+14, window.innerWidth-250))}px`;
    drag.ghost.style.top = `${Math.max(8, Math.min(drag.y+14, window.innerHeight-48))}px`;
    const box = this.root.getBoundingClientRect();
    if (drag.x >= box.left && drag.x <= box.right && drag.y >= box.top && drag.y <= box.bottom) {
      const horizontal = this.horizontal();
      const scroller = horizontal ? this.blocks : this.root;
      const scrollBox = scroller.getBoundingClientRect();
      const point = horizontal ? drag.x : drag.y;
      const start = horizontal ? scrollBox.left : scrollBox.top;
      const end = horizontal ? scrollBox.right : scrollBox.bottom;
      const delta = point < start+32 ? -10 : point > end-32 ? 10 : 0;
      if (horizontal) scroller.scrollLeft += delta; else scroller.scrollTop += delta;
      const before = this.items().filter(item => item !== drag.block).find(item => {
        const rect = item.getBoundingClientRect();
        return point < (horizontal ? (rect.left+rect.right)/2 : (rect.top+rect.bottom)/2);
      }) || null;
      if (drag.block.nextElementSibling !== before) this.blocks.insertBefore(drag.block, before);
    }
    drag.frame = requestAnimationFrame(() => this.dragFrame());
  }

  finishDrag(cancel) {
    const drag = this.drag;
    if (!drag) return;
    this.drag = null;
    cancelAnimationFrame(drag.frame);
    drag.ghost?.remove();
    drag.block.classList.remove("is-dragging");
    this.root.classList.remove("is-reordering");
    if (this.root.hasPointerCapture(drag.pointerID)) this.root.releasePointerCapture(drag.pointerID);
    if (cancel) {
      for (const item of drag.original) this.blocks.append(item);
      this.updateLabels();
    } else if (drag.moving && this.items().some((item, index) => item !== drag.original[index])) {
      this.changed();
    } else if (!cancel && !drag.moving) {
      const now = performance.now(), id = drag.block.dataset.toolBlock;
      if (this.lastClick?.id === id && now-this.lastClick.time < 400) {
        this.lastClick = null; this.toggleExpanded(drag.block);
      } else { this.lastClick = {id, time:now}; }
    }
    if (document.getElementById("editor-dialog").open) drag.block.querySelector("[data-tool-handle]").focus({preventScroll:true});
  }

  keyDown(event) {
    const handle = event.target.closest("[data-tool-handle]");
    if (!handle || this.isLocked() || this.drag) return;
    if (!event.altKey && (event.key === "Enter" || event.key === " ")) {
      event.preventDefault(); if (!event.repeat) this.toggleExpanded(handle.closest("[data-tool-block]")); return;
    }
    if (!event.altKey) return;
    const delta = {ArrowUp:-1, ArrowLeft:-1, ArrowDown:1, ArrowRight:1}[event.key];
    if (!delta) return;
    this.setExpanded(null);
    event.preventDefault();
    const block = handle.closest("[data-tool-block]");
    const items = this.items();
    const index = items.indexOf(block), next = index+delta;
    if (next < 0 || next >= items.length) return;
    this.blocks.insertBefore(block, delta < 0 ? items[next] : items[next].nextElementSibling);
    handle.focus({preventScroll:true});
    handle.scrollIntoView({block:"nearest", inline:"nearest"});
    this.changed();
  }
}

window.SkrinToolPanel = SkrinToolPanel;
