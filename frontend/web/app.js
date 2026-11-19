"use strict";

(() => {
  const byID = (id) => document.getElementById(id);
  let shelf;
	let editor;
	let toolPanel;
	let workspace;
	let appearance;
  const state = {
    bridge: null,
    settings: null,
    allScreenshots: [],
    results: [],
    selected: null,
    importing: false,
    saving: false,
    savingSettings: false,
    configuringRegion: false,
    openingSettings: false,
    loadingLibrary: false,
    libraryRefreshRequested: false,
    searchTimer: null,
    searchEpoch: 0,
    detailEpoch: 0,
    toastTimer: null,
	capabilities: null,
	capturing: false,
	warnings: [],
	detail: null,
	ocrReadiness:null,
	backgroundReadiness:null,
	photoBusy:false,
	photoToken:null,
	photoCancelRequested:false,
	photoSerial:0,
	photoRequestID:0,
	ocr:null,
	ocrDirty:false,
	ocrExpectedHash:"",
	ocrTimer:null,
	editorRevision:0,
	draftSync:Promise.resolve(),
	layoutSync:Promise.resolve(),
  };

  function errorText(error) {
    if (typeof error === "string") return error;
    if (error && typeof error.message === "string") return error.message;
    return "Не удалось выполнить действие. Подробности отсутствуют.";
  }

  function showError(id, error) {
    const target = byID(id);
    target.textContent = errorText(error);
    target.hidden = false;
  }

  function clearError(id) {
    byID(id).hidden = true;
    byID(id).textContent = "";
  }

  function toast(message) {
    window.clearTimeout(state.toastTimer);
    byID("toast").textContent = message;
    byID("toast").hidden = false;
    state.toastTimer = window.setTimeout(() => { byID("toast").hidden = true; }, 4500);
  }

  function validImageURL(url) {
    return typeof url === "string" && url.startsWith("data:image/png;base64,");
  }

  function dateLabel(raw) {
    const date = new Date(raw);
    if (Number.isNaN(date.getTime())) return "Дата не указана";
    return date.toLocaleString("ru-RU", { day: "numeric", month: "short", year: "numeric", hour: "2-digit", minute: "2-digit" });
  }

  function dimensionsLabel(screenshot) {
    if (Number.isFinite(screenshot.width) && Number.isFinite(screenshot.height)) {
      return `${screenshot.width} × ${screenshot.height}`;
    }
    return "PNG";
  }

  function updateImportButtons() {
    const disabled = !state.bridge || state.importing || state.saving;
    byID("import-button").disabled = disabled;
    byID("empty-import-button").disabled = disabled;
	byID("capture-button").disabled = disabled || state.capturing || !state.capabilities?.area_capture?.available;
	byID("capture-screen-button").disabled = disabled || state.capturing || !state.capabilities?.screen_capture?.available;
	updateBackgroundControls();
  }

	function updateCapabilities(capabilities) {
		state.capabilities=capabilities;
		byID("native-status").textContent=[capabilities.tray?.reason,`Область: ${capabilities.shortcut || capabilities.global_shortcut?.reason || "не назначена"}`,`Весь экран: ${capabilities.screen_shortcut || capabilities.global_screen_shortcut?.reason || "не назначена"}`,`Тихий снимок: ${capabilities.silent_shortcut || capabilities.global_silent_shortcut?.reason || "не назначена"}`,capabilities.area_capture?.reason,capabilities.screen_capture?.reason,capabilities.silent_capture?.reason].filter(Boolean).join(". ");
		byID("silent-shortcut").disabled=!capabilities.silent_capture?.available;
		byID("silent-shortcut").title=capabilities.silent_capture?.reason||"Тихий снимок недоступен";
		byID("silent-area-shortcut").disabled=!capabilities.silent_capture?.available;
		byID("select-silent-region").disabled=!capabilities.silent_capture?.available||state.configuringRegion;
		byID("silent-area-shortcut").title=capabilities.silent_capture?.reason||"Тихий снимок недоступен";
		byID("silent-area-native-status").textContent=`Тихая область: ${capabilities.silent_area_shortcut||capabilities.global_silent_area_shortcut?.reason||"комбинация не назначена"}`;
		byID("capture-button").title=capabilities.area_capture?.reason || "Захват недоступен";
		byID("capture-screen-button").title=capabilities.screen_capture?.reason || "Снимок всего экрана недоступен";
		updateImportButtons();
	}

	async function showSelection(selected) {
		if (!selected?.token || !validImageURL(selected.preview_data_url)) throw new Error("Не удалось получить предпросмотр PNG.");
		state.selected=selected;
		state.ocrDirty=false;state.ocrExpectedHash="";state.editorRevision=0;state.draftSync=Promise.resolve();renderOCR({status:"idle"});
		byID("save-screenshot-button").disabled=true;
		byID("editor-image").src=selected.preview_data_url;
		byID("editor-filename").textContent=selected.name || "Снимок";
		byID("screenshot-note").value="";
		clearError("editor-error");
		for (const id of ["settings-dialog","help-dialog","detail-dialog"]) if (byID(id).open) byID(id).close();
		if(!byID("editor-dialog").open) byID("editor-dialog").showModal();
		try {await editor.load(await state.bridge.GetDraftImageData(selected.token));}
		catch(error){showError("editor-error",error);throw error;}
		if(state.selected?.token!==selected.token)return;
		byID("save-screenshot-button").disabled=false;
		updateBackgroundControls();
		if(selected.from_capture)byID("save-screenshot-button").focus({preventScroll:true});else toolPanel.focusFirst();
		if(state.settings?.auto_index&&state.ocrReadiness?.available)void runDraftOCR(false);
	}

	function renderOCR(result){
		state.ocr=result;
		const labels={idle:"OCR ещё не запускался",queued:"OCR ожидает своей очереди",running:"Распознаём текст локально…",ready:"Текст распознан",no_text:"OCR завершён: текст не найден",error:"Ошибка OCR",unavailable:"OCR недоступен",cancelled:"OCR остановлен",manual:"Текст введён вручную"};
		byID("ocr-status").textContent=(labels[result.status]||result.status)+(result.error?`: ${result.error}`:"");
		byID("ocr-raw").value=result.raw_text||"";
		if(!state.ocrDirty)byID("ocr-text").value=result.has_correction?(result.corrected_text||""):(result.raw_text||"");
		byID("cancel-ocr-button").hidden=!["queued","running"].includes(result.status);
		byID("run-ocr-button").disabled=state.photoBusy||!state.ocrReadiness?.available||["queued","running"].includes(result.status);
		let hint="Комментарий необязателен. После сохранения текст распознаётся в фоне; ждать OCR не нужно.";
		if(result.status==="no_text"&&!byID("ocr-text").value.trim())hint="Текст на снимке не найден. Напиши комментарий для дальнейшего поиска по смыслу";
		else if(!state.ocrReadiness?.available)hint="OCR недоступен; для поиска можно добавить комментарий. Сохранить можно и без него.";
		byID("comment-hint").textContent=hint;
	}

	async function runDraftOCR(force){
		if(!state.selected||state.saving||state.photoBusy||!editor.image)return;
		if(force&&state.ocrDirty&&!window.confirm("Распознать заново и заменить ручные исправления текста?"))return;
		const token=state.selected.token,revision=state.editorRevision;
		try{
			if(force){const operation=state.draftSync.then(()=>state.bridge.InvalidateDraftOCR(token,revision));state.draftSync=operation.catch(()=>{});await operation;state.ocrDirty=false;}
			const data=editor.exportDataURL();
			const operation=state.draftSync.then(()=>state.bridge.UpdateDraftPNG(token,data,revision));state.draftSync=operation.catch(()=>{});await operation;
			const result=await state.bridge.StartDraftOCR(token);
			if(state.selected?.token!==token||state.editorRevision!==revision)return;
			state.ocrExpectedHash=result.input_sha256||"";renderOCR(result);
		}catch(error){if(state.selected?.token===token&&state.editorRevision===revision)renderOCR({status:"error",error:errorText(error)});}
	}

	function editorChanged(){
		if(!state.selected||!state.bridge)return;
		state.editorRevision++;state.ocrExpectedHash="";state.ocrDirty=false;renderOCR({status:"idle"});
		const token=state.selected.token,revision=state.editorRevision;window.clearTimeout(state.ocrTimer);
		const operation=state.draftSync.then(()=>state.bridge.InvalidateDraftOCR(token,revision));state.draftSync=operation.catch(()=>{});
		void operation.then(()=>{
			if(state.selected?.token===token&&state.editorRevision===revision&&state.settings?.auto_index&&state.ocrReadiness?.available)state.ocrTimer=window.setTimeout(()=>{void runDraftOCR(false);},650);
		}).catch(error=>showError("editor-error",error));
	}

	async function captureArea(fullScreen=false) {
		if(!state.bridge || state.capturing)return;
		state.capturing=true;updateImportButtons();clearError("page-error");
		try {if(fullScreen)await state.bridge.CaptureScreen();else await state.bridge.CaptureArea();} catch(error) {
			if(!errorText(error).includes("Захват отменён"))showError("page-error",error);
		} finally {state.capturing=false;updateImportButtons();}
	}

	function saveEditorLayout(layout) {
		if(!state.bridge)return;
		const payload={...(layout||toolPanel.layout()),...workspace.layout()};
		const operation=state.layoutSync.then(async()=>{
			const saved=await state.bridge.SaveEditorLayout(payload);
			if(state.settings){
				state.settings.editor_panel_position=saved.position;state.settings.editor_tool_order=saved.tool_order;
				state.settings.editor_panel_width=saved.panel_width;state.settings.editor_panel_height=saved.panel_height;
				state.settings.editor_width=saved.width;state.settings.editor_height=saved.height;
			}
		});
		state.layoutSync=operation.catch(error=>{
			byID("editor-layout-status").textContent="Не удалось сохранить раскладку панели.";
			showError("editor-error",`Раскладка изменена, но не сохранена: ${errorText(error)}`);
		});
	}

  function sortedScreenshots(items) {
    return (Array.isArray(items) ? items : []).slice().sort((first, second) => {
      return (Date.parse(second.created_at) || 0) - (Date.parse(first.created_at) || 0);
    });
  }

  async function refreshLibrary() {
    if(state.loadingLibrary){state.libraryRefreshRequested=true;return;}
    window.clearTimeout(state.searchTimer);
    state.loadingLibrary = true;
    byID("search-input").disabled = true;
    const epoch = ++state.searchEpoch;
    let loaded = false;
    byID("gallery-region").setAttribute("aria-busy", "true");
    try {
      const snapshot = await state.bridge.GetState();
      if (epoch !== state.searchEpoch) return;
      state.settings = snapshot.settings;
      state.allScreenshots = sortedScreenshots(snapshot.screenshots);
	  state.warnings=Array.isArray(snapshot.warnings)?snapshot.warnings:[];
	  byID("library-warnings").hidden=state.warnings.length===0;
	  byID("warnings-summary").textContent=`Обнаружено: ${state.warnings.length}. Остальные снимки доступны; исходные файлы не изменены.`;
      renderIndexStatus();
      byID("nav-count").textContent = String(state.allScreenshots.length);
      byID("library-location").textContent = state.settings.library_directory || "Локальная библиотека";
      byID("library-location").title = state.settings.library_directory || "";
      clearError("page-error");
      loaded = true;
      if (!byID("search-input").value.trim()) {
        state.results = state.allScreenshots;
        renderGallery();
      }
    } catch (error) {
      if (epoch !== state.searchEpoch) return;
      showError("page-error", error);
      byID("result-count").textContent = "Библиотека недоступна";
    } finally {
      state.loadingLibrary = false;
      byID("search-input").disabled = !state.bridge;
      if (epoch === state.searchEpoch) byID("gallery-region").setAttribute("aria-busy", "false");
    }
    if(state.libraryRefreshRequested){state.libraryRefreshRequested=false;await refreshLibrary();}
    else if(loaded&&byID("search-input").value.trim())await runSearch();
  }

  function ocrLabel(result={}) {
    const labels={queued:"OCR в очереди",running:"Распознаём текст…",no_text:"Текст не найден. Добавь комментарий для поиска.",unavailable:"OCR недоступен — открой карточку для подробностей",error:"Ошибка OCR — открой карточку",interrupted:"OCR прерван — запусти его из карточки",cancelled:"OCR остановлен вручную",not_requested:"OCR ещё не запускался",not_implemented:"OCR ещё не запускался",idle:"OCR ещё не запускался"};
    return labels[result.status]||"Без комментария";
  }

  function renderIndexStatus() {
    const count=statuses=>state.allScreenshots.filter(item=>statuses.includes(item.ocr?.status||"not_requested")).length;
    const active=count(["queued","running"]),failed=count(["unavailable","error","interrupted","cancelled"]),missing=count(["not_requested","not_implemented","idle"]);
    const parts=[];
    if(active)parts.push(`OCR в фоне: ${active}. Поиск обновится после распознавания.`);
    if(failed)parts.push(`OCR недоступен, прерван или остановлен у ${failed} снимков — подробности в карточках.`);
    if(missing)parts.push(`Без OCR: ${missing}. Для старых снимков запусти распознавание из карточки.`);
    byID("index-status").textContent=parts.join(" ")||"Поиск по словам распознанного текста и комментариев.";
  }

  async function runSearch() {
    if (state.loadingLibrary) return;
    const query = byID("search-input").value.trim();
    const epoch = ++state.searchEpoch;
    byID("gallery-region").setAttribute("aria-busy", "true");
    byID("result-count").textContent = "Поиск…";
    try {
      const results = query ? await state.bridge.SearchScreenshots(query) : state.allScreenshots;
      if (epoch !== state.searchEpoch) return;
      state.results = sortedScreenshots(results);
      clearError("page-error");
      renderGallery();
    } catch (error) {
      if (epoch !== state.searchEpoch) return;
      showError("page-error", error);
      byID("result-count").textContent = "Поиск не выполнен";
    } finally {
      if (epoch === state.searchEpoch) byID("gallery-region").setAttribute("aria-busy", "false");
    }
  }

  function renderGallery() {
    shelf?.setData(state.results, state.allScreenshots, state.settings, byID("search-input").value.trim());
  }

  async function selectPNG() {
    if (!state.bridge || state.importing || state.saving || state.selected) return;
    state.importing = true;
    updateImportButtons();
    clearError("page-error");
    try {
      const selected = await state.bridge.SelectPNG();
      if (!selected || !selected.token) return;
      state.selected = selected;
	  await showSelection(selected);
    } catch (error) {
      showError("page-error", error);
      discardSelection();
    } finally {
      state.importing = false;
      updateImportButtons();
    }
  }

  function clearSelection() {
	state.photoBusy=false;state.photoToken=null;state.photoCancelRequested=false;state.photoRequestID=0;
	if(editor)editor.locked=false;
	byID("cancel-background-button").hidden=true;byID("ocr-text").disabled=false;
	updateBackgroundControls();
	window.clearTimeout(state.ocrTimer);state.ocrExpectedHash="";state.ocrDirty=false;state.ocr=null;
	if(editor)editor.reset();
    state.selected = null;
    byID("editor-image").removeAttribute("src");
    byID("editor-filename").textContent = "";
    byID("screenshot-note").value = "";
  }

  function discardSelection() {
    const token = state.selected && state.selected.token;
    clearSelection();
    if (token && state.bridge) {
      state.bridge.DiscardSelectedPNG(token).catch((error) => {
        showError("page-error", `Импорт отменён, но не удалось освободить подготовленное изображение: ${errorText(error)}`);
      });
    }
  }

  function cancelImport() {
    if (state.saving) return;
    byID("editor-dialog").close();
    discardSelection();
  }

  async function saveScreenshot() {
    if (!state.selected || state.saving || state.photoBusy || byID("save-screenshot-button").disabled || toolPanel.drag || workspace.gesture) return;
    const selected=state.selected;
    state.saving = true;
	editor.locked=true;
    byID("save-screenshot-button").disabled = true;
    byID("editor-cancel").disabled = true;
    byID("editor-close").disabled = true;
    byID("screenshot-note").disabled = true;
    updateImportButtons();
    clearError("editor-error");
    try {
	  window.clearTimeout(state.ocrTimer);
	  await state.draftSync;
	  await state.bridge.UpdateDraftPNG(state.selected.token,editor.exportDataURL(),state.editorRevision);
	  if(state.ocrDirty)await state.bridge.SaveDraftOCRText(state.selected.token,byID("ocr-text").value);
      await state.bridge.ImportSelectedPNG(state.selected.token, byID("screenshot-note").value);
      clearSelection();
      byID("editor-dialog").close();
      toast("Скриншот сохранён на полку.");
      if(selected.from_capture){
        try{const result=await state.bridge.FinishSavedCapture(selected.token);if(result.message)toast(result.message);}
        catch(error){showError("page-error",`Снимок сохранён. Не удалось свернуть приложение: ${errorText(error)}`);}
      }
      await refreshLibrary();
    } catch (error) {
      showError("editor-error", error);
    } finally {
      state.saving = false;
	  editor.locked=false;
      byID("save-screenshot-button").disabled = false;
      byID("editor-cancel").disabled = false;
      byID("editor-close").disabled = false;
      byID("screenshot-note").disabled = false;
      updateImportButtons();
    }
  }

  async function openSettings() {
    if (!state.bridge) {
      byID("host-notice").hidden = false;
      return;
    }
    if (state.openingSettings || byID("settings-dialog").open) return;
    state.openingSettings = true;
    byID("settings-button").disabled = true;
    try {
      // Settings must remain reachable when scanning a damaged library fails.
      state.settings = await state.bridge.GetSettings();
      byID("library-directory").value = state.settings.library_directory || "";
	  byID("capture-shortcut").value=state.settings.capture_shortcut || "";
	  byID("screen-shortcut").value=state.settings.screen_shortcut || "";
	  byID("silent-shortcut").value=state.capabilities?.silent_capture?.available?(state.settings.silent_shortcut || ""):"";
	  byID("silent-area-shortcut").value=state.capabilities?.silent_capture?.available?(state.settings.silent_area_shortcut || ""):"";
	  renderSilentRegion();
	  byID("start-in-tray").checked=Boolean(state.settings.start_in_tray);
	  byID("auto-index").checked=Boolean(state.settings.auto_index);
	  appearance.begin(state.settings);
      clearError("settings-error");
      byID("settings-dialog").showModal();
    } catch (error) {
      showError("page-error", error);
    } finally {
      state.openingSettings = false;
      byID("settings-button").disabled = false;
    }
  }

  async function selectFolder() {
    if (!state.bridge || state.savingSettings) return;
    byID("select-folder-button").disabled = true;
    clearError("settings-error");
    try {
      const directory = await state.bridge.SelectLibraryDirectory();
      if (directory) byID("library-directory").value = directory;
    } catch (error) {
      showError("settings-error", error);
    } finally {
      byID("select-folder-button").disabled = false;
    }
  }

  function renderSilentRegion() {
    const region=state.settings?.silent_region;
    byID("silent-region-status").textContent=region?`Область: ${region.width} × ${region.height} px, X=${region.x}, Y=${region.y}.` : "Область ещё не задана.";
  }

  async function selectSilentRegion() {
    if(!state.bridge||state.configuringRegion||state.savingSettings)return;
    state.configuringRegion=true;
    byID("select-silent-region").disabled=true;byID("save-settings-button").disabled=true;
    const closeButtons=byID("settings-dialog").querySelectorAll("[data-close-dialog]");
    for(const button of closeButtons)button.disabled=true;
    clearError("settings-error");
    try{
      const region=await state.bridge.SelectSilentRegion();
      state.settings.silent_region=region;renderSilentRegion();
      toast("Область тихого снимка сохранена. Назначь комбинацию и сохрани настройки.");
    }catch(error){if(!errorText(error).includes("Захват отменён"))showError("settings-error",error);}
    finally{
      state.configuringRegion=false;byID("save-settings-button").disabled=false;
      byID("select-silent-region").disabled=!state.capabilities?.silent_capture?.available;
      for(const button of closeButtons)button.disabled=false;
    }
  }

  async function saveSettings() {
    if (!state.settings || state.savingSettings || state.configuringRegion) return;
    state.savingSettings = true;
    byID("save-settings-button").disabled = true;
    byID("select-folder-button").disabled = true;
    const settingsCloseButtons = byID("settings-dialog").querySelectorAll("[data-close-dialog]");
    for (const button of settingsCloseButtons) button.disabled = true;
    clearError("settings-error");
    try {
      state.settings = await state.bridge.SaveSettings({ ...state.settings, ...appearance.draft(), library_directory: byID("library-directory").value,capture_shortcut:byID("capture-shortcut").value,screen_shortcut:byID("screen-shortcut").value,silent_shortcut:byID("silent-shortcut").value,silent_area_shortcut:byID("silent-area-shortcut").value,start_in_tray:byID("start-in-tray").checked,auto_index:byID("auto-index").checked });
      appearance.commit(state.settings);
      byID("settings-dialog").close();
      byID("search-input").value = "";
      window.clearTimeout(state.searchTimer);
      await refreshLibrary();
      toast("Настройки сохранены.");
    } catch (error) {
      showError("settings-error", error);
    } finally {
      state.savingSettings = false;
      byID("save-settings-button").disabled = false;
      byID("select-folder-button").disabled = false;
      for (const button of settingsCloseButtons) button.disabled = false;
    }
  }

  async function openDetail(screenshot) {
	state.detail=screenshot;
	clearError("detail-error");
    const epoch = ++state.detailEpoch;
    byID("detail-title").textContent = screenshot.image_file || "Скриншот";
    byID("detail-note").value = screenshot.note || "";
	updateDetailOCR(screenshot.ocr);
    byID("detail-meta").textContent = `${dateLabel(screenshot.created_at)}\n${dimensionsLabel(screenshot)} · ${window.ShelfModel.size(screenshot.image_bytes)} · PNG + JSON`;
    byID("detail-image").hidden = true;
    byID("detail-image").removeAttribute("src");
    byID("detail-image-status").textContent = "Загрузка изображения…";
    byID("detail-image-status").hidden = false;
    byID("detail-dialog").showModal();
    try {
      const url = await state.bridge.GetImageData(screenshot.id);
      if (epoch !== state.detailEpoch || !byID("detail-dialog").open) return;
      if (!validImageURL(url)) throw new Error("Приложение не вернуло PNG для предпросмотра.");
      byID("detail-image").src = url;
      byID("detail-image").hidden = false;
      byID("detail-image-status").hidden = true;
    } catch (error) {
      if (epoch !== state.detailEpoch || !byID("detail-dialog").open) return;
      byID("detail-image-status").textContent = errorText(error);
    }
  }

  function attachEvents() {
	byID("remove-background-button").addEventListener("click",()=>{void removeBackground();});
	byID("cancel-background-button").addEventListener("click",async()=>{
		if(!state.photoToken)return;state.photoCancelRequested=true;
		byID("background-status").textContent="Останавливаем удаление фона…";
		try{await state.bridge.CancelBackgroundRemoval(state.photoToken,state.photoRequestID);}catch(error){showError("editor-error",error);}
	});
	window.addEventListener("keydown",event=>{if(event.key==="F1"){event.preventDefault();if(!byID("help-dialog").open)byID("help-dialog").showModal();}});
	byID("editor-dialog").addEventListener("keydown",event=>{
		if(event.defaultPrevented||event.key!=="Enter"||event.repeat||event.isComposing||event.ctrlKey||event.altKey||event.shiftKey||event.metaKey)return;
		if(event.target.isContentEditable||event.target.closest('textarea,input,select,button,a,summary,[role="separator"],[data-tool-handle]'))return;
		event.preventDefault();void saveScreenshot();
	});
	byID("detail-run-ocr").addEventListener("click",async()=>{if(!state.detail)return;try{const result=await state.bridge.RunRecordOCR(state.detail.id);updateDetailOCR(result);}catch(error){showError("detail-error",error);}});
	byID("detail-copy-ocr").addEventListener("click",async()=>{try{await state.bridge.CopyOCRText(byID("detail-ocr-text").value);toast("Текст скопирован.");}catch(error){showError("detail-error",error);}});
	byID("detail-export-ocr").addEventListener("click",async()=>{try{await state.bridge.ExportOCRText(byID("detail-ocr-text").value);}catch(error){showError("detail-error",error);}});
	byID("run-ocr-button").addEventListener("click",()=>{void runDraftOCR(true);});
	byID("cancel-ocr-button").addEventListener("click",async()=>{if(!state.selected)return;const token=state.selected.token;try{await state.bridge.CancelDraftOCR(token);if(state.selected?.token===token){state.ocrExpectedHash="";renderOCR({...state.ocr,status:"cancelled"});}}catch(error){showError("editor-error",error);}});
	byID("ocr-text").addEventListener("input",()=>{state.ocrDirty=true;});
	byID("copy-ocr-button").addEventListener("click",async()=>{try{await state.bridge.CopyOCRText(byID("ocr-text").value);toast("Текст скопирован.");}catch(error){showError("editor-error",error);}});
	byID("export-ocr-button").addEventListener("click",async()=>{try{if(await state.bridge.ExportOCRText(byID("ocr-text").value))toast("Текст сохранён в TXT.");}catch(error){showError("editor-error",error);}});
	byID("save-note-button").addEventListener("click",async()=>{
		if(!state.detail)return;
		byID("save-note-button").disabled=true;clearError("detail-error");
		try{state.detail=await state.bridge.UpdateNote(state.detail.id,state.detail.metadata_digest,byID("detail-note").value);toast("Комментарий сохранён.");await refreshLibrary();}
		catch(error){showError("detail-error",error);}
		finally{byID("save-note-button").disabled=false;}
	});
	byID("show-recovery-button").addEventListener("click",()=>{
		const list=byID("recovery-list");list.replaceChildren();
		for(const warning of state.warnings){
			const row=document.createElement("section"),title=document.createElement("h3"),message=document.createElement("p");
			title.textContent=warning.file;message.textContent=warning.message;row.append(title,message);
			if(warning.kind==="orphan_png"){
				const button=document.createElement("button");button.className="button button-secondary";button.textContent="Импортировать новую копию";
				button.addEventListener("click",async()=>{button.disabled=true;try{await state.bridge.RecoverOrphan(warning.file,"");toast("Создана новая пара PNG + JSON. Исходный PNG сохранён.");byID("recovery-dialog").close();await refreshLibrary();}catch(error){message.textContent=errorText(error);button.disabled=false;}});row.append(button);
			}
			list.append(row);
		}
		byID("recovery-dialog").showModal();
	});
	byID("capture-button").addEventListener("click",()=>{void captureArea();});
	byID("capture-screen-button").addEventListener("click",()=>{void captureArea(true);});
	byID("quit-button").addEventListener("click",()=>{if(state.bridge)void state.bridge.Quit();});
    byID("import-button").addEventListener("click", () => { void selectPNG(); });
    byID("empty-import-button").addEventListener("click", () => { void selectPNG(); });
    byID("save-screenshot-button").addEventListener("click", () => { void saveScreenshot(); });
    byID("editor-cancel").addEventListener("click", cancelImport);
    byID("editor-close").addEventListener("click", cancelImport);
    byID("editor-dialog").addEventListener("cancel", (event) => { event.preventDefault(); cancelImport(); });
    byID("settings-button").addEventListener("click", () => { void openSettings(); });
    byID("select-folder-button").addEventListener("click", () => { void selectFolder(); });
    byID("select-silent-region").addEventListener("click", () => { void selectSilentRegion(); });
    byID("save-settings-button").addEventListener("click", () => { void saveSettings(); });
    byID("settings-dialog").addEventListener("cancel", (event) => { if (state.savingSettings||state.configuringRegion) event.preventDefault(); });
    byID("help-button").addEventListener("click", () => { byID("help-dialog").showModal(); });
    byID("detail-dialog").addEventListener("close", () => {
      state.detailEpoch += 1;
      byID("detail-image").removeAttribute("src");
    });
    const resetSearch = () => {
      byID("search-input").value = "";
      window.clearTimeout(state.searchTimer);
      if (state.bridge && !state.loadingLibrary) void runSearch();
    };
    byID("clear-search-button").addEventListener("click", resetSearch);
    byID("library-nav").addEventListener("click", resetSearch);
    document.querySelector(".brand").addEventListener("click", (event) => { event.preventDefault(); resetSearch(); });
    byID("search-input").addEventListener("input", () => {
      window.clearTimeout(state.searchTimer);
      // Invalidate an in-flight request as soon as the query changes.
      state.searchEpoch += 1;
      state.searchTimer = window.setTimeout(() => { void runSearch(); }, 220);
    });
    for (const button of document.querySelectorAll("[data-close-dialog]")) {
      button.addEventListener("click", () => { byID(button.dataset.closeDialog).close(); });
    }
  }

  async function initialize() {
	editor=new window.SkrinEditor(byID("editor-canvas"),editorChanged);
	toolPanel=new window.SkrinToolPanel(saveEditorLayout,()=>state.saving);
	workspace=new window.SkrinWorkspace(()=>saveEditorLayout(),()=>state.saving||Boolean(toolPanel.drag),async expanded=>{if(state.bridge)await state.bridge.SetWorkspaceMaximised(expanded);});
	appearance=new window.SkrinAppearance(async value=>{
		if(!state.bridge)throw new Error("Нет подключения к ядру.");
		const saved=await state.bridge.SaveAppearance(value);
		if(state.settings)Object.assign(state.settings,saved);
		return saved;
	},error=>showError("page-error",error));
    attachEvents();
    state.bridge = window.go && window.go.main && window.go.main.Desktop;
    if (!state.bridge) {
      byID("host-notice").hidden = false;
      byID("result-count").textContent = "Нет подключения к ядру";
      byID("gallery-region").setAttribute("aria-busy", "false");
      byID("empty-state").hidden = false;
      return;
    }
    shelf = new window.ScreenShortsShelf({bridge:()=>state.bridge,open:item=>{void openDetail(item);},refresh:refreshLibrary,error:error=>showError("page-error",error),toast,date:dateLabel,ocrLabel});
    updateImportButtons();
	if(window.runtime?.EventsOn){
		window.runtime.EventsOn("capture:ready",selected=>{void showSelection(selected).catch(error=>showError("page-error",error));});
		window.runtime.EventsOn("native:capabilities",updateCapabilities);
		window.runtime.EventsOn("native:error",error=>showError("page-error",error));
		window.runtime.EventsOn("ocr:warning",error=>{byID("ocr-warning").textContent=errorText(error);byID("ocr-warning").hidden=false;});
		window.runtime.EventsOn("background:update",event=>{if(state.photoToken===event.token&&state.photoRequestID===event.request_id&&!state.photoCancelRequested)byID("background-status").textContent=event.status==="running"?"Вырезаем фон локально…":"Ожидаем завершения другой обработки…";});
		window.runtime.EventsOn("capture:silent-saved",()=>{void refreshLibrary();});
		window.runtime.EventsOn("capture:silent-error",error=>{byID("silent-error").textContent=errorText(error);byID("silent-error").hidden=false;});
		window.runtime.EventsOn("native:settings",()=>{void openSettings();});
		window.runtime.EventsOn("native:resume-draft",()=>{if(state.selected&&!byID("editor-dialog").open)byID("editor-dialog").showModal();});
		window.runtime.EventsOn("ocr:update",event=>{
			if(state.selected?.token===event.token&&state.ocrExpectedHash&&event.result.input_sha256===state.ocrExpectedHash)renderOCR(event.result);
			if(state.detail?.id===event.record_id&&byID("detail-dialog").open)updateDetailOCR(event.result);
			if(event.record_id)void refreshLibrary();
		});
	}
	state.settings=await state.bridge.GetSettings();
	toolPanel.apply({position:state.settings.editor_panel_position,tool_order:state.settings.editor_tool_order});
	workspace.apply({panel_width:state.settings.editor_panel_width,panel_height:state.settings.editor_panel_height,width:state.settings.editor_width,height:state.settings.editor_height});
	appearance.commit(state.settings);
	state.ocrReadiness=await state.bridge.GetOCRReadiness();
	byID("ocr-readiness").textContent=state.ocrReadiness.reason;
	state.backgroundReadiness=await state.bridge.GetBackgroundReadiness();
	byID("background-readiness").textContent=state.backgroundReadiness.reason;
	updateBackgroundControls();
	updateCapabilities(await state.bridge.GetCapabilities());
    await refreshLibrary();
  }

  void initialize();

  function updateBackgroundControls(){
    byID("remove-background-button").disabled=!state.bridge||!state.selected||!state.backgroundReadiness?.available||state.photoBusy||state.saving;
    if(!state.photoBusy)byID("background-status").textContent=state.backgroundReadiness?.reason||"Определяем доступность удаления фона…";
  }

  async function removeBackground(){
    if(!state.selected||state.saving||state.photoBusy||!state.backgroundReadiness?.available||!editor.image||editor.gesture)return;
    if(!editor.canRemoveBackground()){showError("editor-error","История удаления фона заполнена. Отмени предыдущее удаление или сохрани снимок.");return;}
    const token=state.selected.token,revision=state.editorRevision,requestID=++state.photoSerial;
    state.photoBusy=true;state.photoToken=token;state.photoCancelRequested=false;state.photoRequestID=requestID;
    editor.locked=true;editor.buttons();window.clearTimeout(state.ocrTimer);clearError("editor-error");
    if(["queued","running"].includes(state.ocr?.status)){state.ocrExpectedHash="";renderOCR({...state.ocr,status:"idle"});}
    byID("save-screenshot-button").disabled=true;byID("ocr-text").disabled=true;byID("run-ocr-button").disabled=true;
    byID("cancel-background-button").hidden=false;updateBackgroundControls();
    byID("background-status").textContent="Подготавливаем текущий снимок…";
    try{
      await state.draftSync;
      if(state.selected?.token!==token||state.photoCancelRequested)return;
      await state.bridge.UpdateDraftPNG(token,editor.exportDataURL(),revision);
      if(state.selected?.token!==token||state.photoCancelRequested)return;
      const result=await state.bridge.RemoveDraftBackground(token,revision,requestID);
      if(state.selected?.token!==token||state.editorRevision!==revision||state.photoCancelRequested)return;
      if(!validImageURL(result.png))throw new Error("Некорректный результат удаления фона.");
      if(await editor.applyBackground(result.png,()=>state.selected?.token===token&&state.editorRevision===revision&&!state.photoCancelRequested))toast("Фон удалён. «Отменить» вернёт прежнее изображение.");
      else if(state.photoCancelRequested)toast("Удаление фона остановлено.");
    }catch(error){
      if(state.selected?.token===token){if(state.photoCancelRequested)toast("Удаление фона остановлено.");else showError("editor-error",error);}
    }finally{
      if(state.photoToken===token){
        state.photoBusy=false;state.photoToken=null;state.photoRequestID=0;editor.locked=state.saving;editor.buttons();
        byID("save-screenshot-button").disabled=state.saving||!state.selected;
        byID("ocr-text").disabled=false;byID("cancel-background-button").hidden=true;
        byID("run-ocr-button").disabled=!state.ocrReadiness?.available;
        updateBackgroundControls();
        if(state.selected&&state.settings?.auto_index&&state.ocrReadiness?.available)void runDraftOCR(false);
      }
    }
  }

	function updateDetailOCR(result={}){
		byID("detail-ocr-text").value=result.has_correction?(result.corrected_text||""):(result.raw_text||"");
		const labels={not_requested:"OCR ещё не запускался",not_implemented:"OCR ещё не запускался",queued:"Ожидает очереди",running:"Распознаём локально…",ready:"Текст распознан",no_text:"Текст на снимке не найден. Напиши комментарий для дальнейшего поиска по смыслу",error:"Ошибка OCR",cancelled:"OCR остановлен",interrupted:"Предыдущая обработка прервана; можно запустить заново",unavailable:"OCR недоступен",manual:"Текст исправлен вручную"};
		byID("detail-ocr-status").textContent=(labels[result.status]||"OCR ещё не запускался")+(result.error?`: ${result.error}`:"");
		byID("detail-run-ocr").disabled=!state.ocrReadiness?.available||["queued","running"].includes(result.status);
	}
})();
