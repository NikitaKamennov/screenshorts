"use strict";

class SkrinAppearance {
  constructor(save, reportError) {
    this.save = save; this.reportError = reportError;
    this.button = document.getElementById("theme-toggle");
    this.theme = document.getElementById("appearance-theme");
    this.color = document.getElementById("accent-color");
    this.value = {theme:"light", accent_color:"#2d6a4e"};
    this.persisted = {...this.value};
    this.button.addEventListener("click", () => { void this.toggle(); });
    this.theme.addEventListener("change", () => this.preview());
    this.color.addEventListener("input", () => this.preview());
    for (const button of document.querySelectorAll("[data-accent]")) button.addEventListener("click", () => {
      this.color.value = button.dataset.accent; this.preview();
    });
    document.getElementById("settings-dialog").addEventListener("close", () => this.apply(this.persisted));
    this.apply(this.value);
  }

  rgb(hex) { return [1,3,5].map(at => parseInt(hex.slice(at, at+2), 16)); }
  mix(a, b, amount) { return a.map((part, index) => Math.round(part*(1-amount)+b[index]*amount)); }
  css(parts) { return `rgb(${parts.join(",")})`; }
  luminance(rgb) {
    const parts = rgb.map(part => { const x=part/255; return x<=0.04045?x/12.92:((x+0.055)/1.055)**2.4; });
    return parts[0]*.2126+parts[1]*.7152+parts[2]*.0722;
  }
  contrast(a, b) { const x=this.luminance(a), y=this.luminance(b); return (Math.max(x,y)+.05)/(Math.min(x,y)+.05); }

  apply(value) {
    const theme = value.theme === "dark" ? "dark" : "light";
    const accent = /^#[0-9a-f]{6}$/i.test(value.accent_color || "") ? value.accent_color.toLowerCase() : "#2d6a4e";
    this.value = {theme, accent_color:accent};
    const dark = theme === "dark", root = document.documentElement;
    root.dataset.theme = theme; root.style.colorScheme = theme;
    document.querySelector('meta[name="color-scheme"]').content = theme;
    const rgb = this.rgb(accent), surface = this.rgb(dark?"#1c242e":"#ffffff");
    const on = this.contrast(rgb,[255,255,255]) >= this.contrast(rgb,[0,0,0]) ? [255,255,255] : [0,0,0];
    let text = rgb;
    const target = dark?[255,255,255]:[0,0,0];
    for (let step=0; step<=20 && this.contrast(text,surface)<4.5; step++) text=this.mix(rgb,target,step/20);
    const variables = {
      "--accent-fill":accent, "--green":this.css(text), "--on-accent":this.css(on),
      "--accent-hover":this.css(this.mix(rgb,on,.12)),
      "--light-green":this.css(this.mix(surface,rgb,dark?.22:.1)),
      "--accent-mid":this.css(this.mix(surface,rgb,.3)), "--focus":this.css(this.mix(surface,text,.65)),
    };
    for (const [name, color] of Object.entries(variables)) root.style.setProperty(name,color);
    this.button.textContent = dark?"☀ Светлая тема":"☾ Тёмная тема";
    this.button.setAttribute("aria-pressed", String(dark));
    this.button.title = dark?"Включить светлую тему":"Включить тёмную тему";
    document.getElementById("accent-value").textContent = accent;
    for (const button of document.querySelectorAll("[data-accent]")) button.setAttribute("aria-pressed", String(button.dataset.accent.toLowerCase()===accent));
  }

  commit(settings) { this.persisted = {theme:settings.theme, accent_color:settings.accent_color}; this.apply(this.persisted); }
  begin(settings) { this.commit(settings); this.theme.value=this.value.theme; this.color.value=this.value.accent_color; }
  draft() { return {theme:this.theme.value, accent_color:this.color.value}; }
  preview() { this.apply(this.draft()); }
  async toggle() {
    if (this.button.disabled) return;
    const previous = {...this.persisted};
    const next = {...previous, theme:previous.theme==="dark"?"light":"dark"};
    this.button.disabled = true; this.apply(next);
    try { this.commit(await this.save(next)); }
    catch(error) { this.apply(previous); this.reportError(error); }
    finally { this.button.disabled=false; }
  }
}

window.SkrinAppearance = SkrinAppearance;
