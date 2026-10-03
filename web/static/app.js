// Vivid Mistress — single always-on chat front end.
(function () {
  'use strict';

  // ---- Console helpers ---------------------------------------------------
  // Every step is logged to the browser console (F12) so it is easy to see
  // exactly what the page is doing: message sending, mp3 playback, button
  // generation, button clicks and the auto-continue timer.
  function log() { console.log('[Vivid Mistress]', ...arguments); }
  function warn() { console.warn('[Vivid Mistress]', ...arguments); }
  function errorLog() { console.error('[Vivid Mistress]', ...arguments); }

  const $ = (id) => document.getElementById(id);

  async function api(path, options) {
    options = options || {};
    const headers = Object.assign({
      'Content-Type': 'application/json',
      'X-Client-ID': getClientId(),
    }, options.headers || {});
    const opts = { method: options.method || 'GET', ...options, headers };
    if (opts.body && typeof opts.body !== 'string') opts.body = JSON.stringify(opts.body);
    const resp = await fetch(path, opts);
    const data = await resp.json().catch(() => ({}));
    if (!resp.ok) throw new Error(data.error || 'Request failed (' + resp.status + ')');
    return data;
  }

  // ---- State --------------------------------------------------------------
  const state = {
    convId: null,
    conv: null,
    sending: false,
    role: '',
  };

  let voiceOn = true;                  // spoken (mp3) replies on/off
  let voiceName = 'en-GB-SoniaNeural'; // internal voice id
  let voiceFriendly = '';              // external (friendly) voice name e.g. Sonia
  let voiceLabel = '';                 // e.g. "Sonia — British, Clear/Expressive"
  let voiceSpeed = 3;
  let suggestionButtons = [];          // currently displayed button labels
  let responseTimer = null;            // auto-continue timeout id
  let audioPlayer = null;
  let autoStreak = 0;
  let autoContinueOn = true;
  let autoDelaySeconds = 10;
  let autoListenOn = true;
  let autoListenDelayMs = 800;
  let autoListenTimer = null;
  function clearAutoListenTimer() { if (autoListenTimer) { log('cancelling pending auto-mic start'); clearTimeout(autoListenTimer); autoListenTimer = null; } }
  // Hands-free mode: when an mp3 finishes, open the mic automatically so the
  // user can just speak. NOTE: scheduleAutoListen is a function declaration
  // (hoisted), so handleAutoReply can call it even though the code lives
  // further down next to the mic code.
  function scheduleAutoListen(afterMs, why) {
    clearAutoListenTimer();
    if (!autoListenOn) { log('auto-mic skipped: listen off (say `start` or `listen`)'); return; }
    if (!voiceOn) { log('auto-mic skipped: voice off (no mp3 flow)'); return; }
    if (typeof micSupported === 'function' && !micSupported()) { log('auto-mic skipped: not supported'); return; }
    if (!window.isSecureContext) { log('auto-mic skipped: insecure context'); return; }
    if (state.localCmd) { log('auto-mic skipped: command reply'); return; }
    if (state.sending) { log('auto-mic skipped: send in flight'); return; }
    var delay = (afterMs == null ? autoListenDelayMs : afterMs);
    log('auto-mic armed in ' + delay + 'ms (' + (why || 'mp3 ended') + ')');
    autoListenTimer = setTimeout(function () {
      autoListenTimer = null;
      if (!autoListenOn || micListening || state.sending) return;
      log('auto-mic starting (like pressing MIC)');
      startMic(true);
    }, delay);
  }
  let suggestionCount = 5;             // suggestion buttons to show (1-5)
  let textOn = true;                   // show reply text on screen ("text on/off")

  const MAX_AUTO_CONTINUES = 3;        // safety cap so the chat never runs away
  const DEFAULT_AUTO_DELAY = 10;       // default pause after the mp3 (seconds)
  const MAX_AUTO_DELAY = 300;          // largest allowed "delay [n]" (seconds)
  const MAX_SUGGESTION_BUTTONS = 5;    // largest allowed "buttons [n]"

  const KEY_CLIENT = 'vm_client_id';
  const KEY_VOICE = 'vm_voice_pref';
  const KEY_PUSH = 'vm_push_on';       // '1' = auto-continue on, '0' = off
  const KEY_DELAY = 'vm_push_delay';   // seconds to wait after the mp3
  const KEY_BUTTONS = 'vm_buttons';    // suggestion button count (1-5)
  const KEY_TEXT = 'vm_text_on';       // '1' = text shown, '0' = text hidden
  const KEY_LISTEN = 'vm_listen_on';   // '1' = auto-mic after mp3, '0' = off

  // ---- Per-browser client id ---------------------------------------------
  function getClientId() {
    let id = null;
    try { id = localStorage.getItem(KEY_CLIENT); } catch (e) { /* ignore */ }
    if (!id) {
      id = 'client_' + Date.now().toString(36) + '_' + Math.random().toString(36).slice(2, 10);
      try { localStorage.setItem(KEY_CLIENT, id); } catch (e) { /* ignore */ }
      log('created new client id', id);
    }
    return id;
  }

  // ---- Per-browser session preferences ------------------------------------
  function readLocalInt(key, dflt) {
    try {
      const v = parseInt(localStorage.getItem(key), 10);
      if (!Number.isNaN(v)) return v;
    } catch (e) { /* ignore */ }
    return dflt;
  }

  function saveLocalStr(key, value) {
    try { localStorage.setItem(key, String(value)); } catch (e) { /* ignore */ }
  }

  // Restores the auto-continue ("push") settings for this browser: whether
  // pushing is on, how many seconds to wait after the voice reply, and how
  // many suggestion buttons to show.
  function loadSessionPrefs() {
    try {
      if (localStorage.getItem(KEY_PUSH) === '0') autoContinueOn = false;
      if (localStorage.getItem(KEY_TEXT) === '0') textOn = false;
      if (localStorage.getItem(KEY_LISTEN) === '0') autoListenOn = false;
    } catch (e) { /* ignore */ }
    autoDelaySeconds = Math.min(Math.max(0, readLocalInt(KEY_DELAY, DEFAULT_AUTO_DELAY)), MAX_AUTO_DELAY);
    suggestionCount = Math.min(Math.max(1, readLocalInt(KEY_BUTTONS, MAX_SUGGESTION_BUTTONS)), MAX_SUGGESTION_BUTTONS);
    log('session prefs: auto-continue=' + (autoContinueOn ? 'on' : 'off') +
      ' delay=' + autoDelaySeconds + 's buttons=' + suggestionCount +
      ' text=' + (textOn ? 'on' : 'off') + ' listen=' + (autoListenOn ? 'on' : 'off'));
  }

  // ---- DOM refs -----------------------------------------------------------
  const el = {
    chatMessages: $('chat-messages'),
    emptyState: $('empty-state'),
    inputMessage: $('input-message'),
    btnSend: $('btn-send'),
    btnMic: $('btn-mic'),
    suggestionButtons: $('suggestion-buttons'),
    btnVoice: $('btn-voice'),
    roleChip: $('role-chip'),
    voiceChip: $('voice-chip'),
    speedChip: $('speed-chip'),
  };

  // ---- Markdown rendering (lightweight, always escaped) -------------------
  function escapeHtml(s) {
    return String(s)
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;')
      .replace(/'/g, '&#39;');
  }

  function renderMarkdown(src) {
    const escaped = escapeHtml(src);
    let html = escaped;

    // Protect fenced code blocks, then style the rest of the text.
    const preBlocks = [];
    html = html.replace(/```([\s\S]*?)```/g, function (m, code) {
      preBlocks.push('<pre class="md-pre"><code>' + code.replace(/^[\r\n]+|[\r\n]+$/g, '') + '</code></pre>');
      return '\u0000PRE' + (preBlocks.length - 1) + '\u0000';
    });
    html = html.replace(/`([^`]+)`/g, '<code class="md-code-inline">$1</code>');
    html = html.replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>');
    html = html.replace(/(^|[\s(])\*([^*\n]+)\*/g, '$1<em>$2</em>');

    // Remove unwanted line feeds: the LLM often emits runs of blank lines
    // ("\n\n\n...") which would open big vertical gaps inside a bubble.
    // Collapse every whitespace-only line down to a single line break.
    html = html.replace(/\r\n?/g, '\n');
    html = html.replace(/[ \t]*\n[ \t]*/g, '\n');
    html = html.replace(/\n+/g, '\n');
    html = html.replace(/^\n+|\n+$/g, '');
    html = html.replace(/\n/g, '<br>');
    html = html.replace(/\u0000PRE(\d+)\u0000/g, function (m, i) {
      return preBlocks[Number(i)];
    });

    // Drop stray breaks caused by leading/trailing newlines.
    const body = html.replace(/^(<br>)+/, '').replace(/(<br>)+$/, '');
    return '<p>' + body + '</p>';
  }

  // ---- Message rendering ---------------------------------------------------
  function scrollToBottom() {
    if (el.chatMessages) el.chatMessages.scrollTop = el.chatMessages.scrollHeight;
  }

  function createMessageEl(role, content, extra) {
    extra = extra || {};
    const div = document.createElement('div');
    div.className = 'message ' + role + (extra.notice ? ' notice' : '') + (extra.error ? ' error' : '');

    const avatar = document.createElement('div');
    avatar.className = 'message-avatar';
    avatar.textContent = role === 'user' ? 'U' : (extra.notice ? 'I' : 'V');
    div.appendChild(avatar);

    const contentDiv = document.createElement('div');
    contentDiv.className = 'message-content' + (role === 'assistant' ? ' md' : '');

    if (role === 'assistant') {
      contentDiv.innerHTML = renderMarkdown(content || '');
    } else {
      contentDiv.textContent = content || '';
    }

    // User bubbles are direct children of .message.user (the avatar is pushed
    // to the far side with CSS) so the bubble can size to its own text and
    // never gets crushed into a narrow column that breaks words mid-way.
    if (role === 'user') {
      div.appendChild(contentDiv);
    } else {
      const body = document.createElement('div');
      body.className = 'message-body';
      body.appendChild(contentDiv);
      div.appendChild(body);
    }

    // No per-message action buttons — the response mp3 plays automatically.
    return div;
  }

  function appendMessage(role, content, extra) {
    if (el.emptyState) el.emptyState.style.display = 'none';
    const node = createMessageEl(role, content, extra || {});
    if (el.chatMessages) {
      el.chatMessages.appendChild(node);
      scrollToBottom();
    }
    return node;
  }

  function renderMessages(messages) {
    const list = messages || [];
    if (!el.chatMessages) return;
    // Remove only rendered message nodes — keep #empty-state in the DOM so it
    // can be shown again after the context is cleared.
    el.chatMessages.querySelectorAll('.message').forEach((n) => n.remove());
    if (!list.length) {
      if (el.emptyState) el.emptyState.style.display = '';
      return;
    }
    if (el.emptyState) el.emptyState.style.display = 'none';
    for (let i = 0; i < list.length; i++) {
      const m = list[i];
      // With "text off" the narration bubbles are hidden, but the voice mp3
      // still plays and the suggestion buttons are still shown.
      if (m.role === 'assistant' && !textOn) continue;
      el.chatMessages.appendChild(createMessageEl(m.role, m.content));
    }
    scrollToBottom();
  }

  // ---- Voice ----------------------------------------------------------------
  // updateStatus refreshes the role / voice / speed chips in the status bar.
  // Only external names are shown (e.g. "Sonia"), never the internal ids.
  function updateStatus() {
    if (el.roleChip) {
      const roleName = state.role || '—';
      el.roleChip.textContent = 'Role: ' + roleName;
      el.roleChip.title = 'Current role: ' + roleName + '. Type \'role\' to list roles.';
    }
    const shownVoice = voiceFriendly || voiceName;
    if (el.voiceChip) {
      el.voiceChip.textContent = (voiceOn ? '\u{1F50A} ' : '\u{1F507} ') + (shownVoice || '—');
      el.voiceChip.title = 'Voice: ' + (voiceLabel || shownVoice || '—') +
        (voiceOn ? ' · spoken replies ON' : ' · spoken replies OFF');
    }
    if (el.speedChip) {
      const mult = (1 + voiceSpeed / 10).toFixed(1);
      el.speedChip.textContent = 'Speed: ' + voiceSpeed + ' (' + mult + 'x)';
      el.speedChip.title = 'Speech speed ' + voiceSpeed + ' = ' + mult + 'x. Type "speed <1-9>" to change.';
    }
  }

  // applyVoiceState copies the voice fields a server response carries.
  function applyVoiceState(data) {
    if (typeof data.voice === 'boolean') voiceOn = data.voice;
    if (data.voice_name) voiceName = data.voice_name;
    if (data.voice_display) voiceFriendly = data.voice_display;
    if (data.voice_label) voiceLabel = data.voice_label;
    if (data.voice_speed) voiceSpeed = data.voice_speed;
    updateStatus();
  }

  function renderVoiceButton() {
    if (!el.btnVoice) return;
    el.btnVoice.textContent = voiceOn ? '\u{1F50A}' : '\u{1F507}';
    el.btnVoice.title = voiceOn ? 'Spoken replies are ON — click to mute' : 'Spoken replies are OFF — click to unmute';
    el.btnVoice.setAttribute('aria-pressed', String(voiceOn));
    el.btnVoice.classList.toggle('muted', !voiceOn);
    updateStatus();
  }

  async function loadVoice() {
    try {
      const data = await api('/api/voice');
      applyVoiceState(data);
      let pref = null;
      try { pref = localStorage.getItem(KEY_VOICE); } catch (e) { /* ignore */ }
      if (pref === null) {
        // First visit: default spoken replies to ON (mp3 is the core flow).
        if (data.voice !== true) {
          log('defaulting voice to ON for this browser');
          await setVoicePref(true);
          return;
        }
      }
      renderVoiceButton();
      log('voice state loaded:', voiceOn, voiceFriendly || voiceName, 'speed', voiceSpeed);
    } catch (e) {
      warn('failed to load voice state:', e.message);
      renderVoiceButton();
    }
  }

  async function setVoicePref(on) {
    const data = await api('/api/voice', { method: 'PUT', body: { voice: !!on } });
    applyVoiceState(data);
    try { localStorage.setItem(KEY_VOICE, voiceOn ? '1' : '0'); } catch (e) { /* ignore */ }
    renderVoiceButton();
    log('voice preference saved:', voiceOn);
  }

  // ---- Audio ----------------------------------------------------------------
  // Each assistant reply is played automatically when the server returns an
  // mp3. There are no per-message audio buttons any more.

  // playAudio plays a base64 mp3 and calls onEnded when playback finishes
  // (or immediately when there is nothing to play / it fails). onMetadata
  // (optional) is called with the mp3 duration in seconds when known, so the
  // auto-continue timer can be armed as soon as the length is available.
  function playAudio(base64Data, mimeType, onEnded, onMetadata) {
    const done = () => { if (onEnded) onEnded(); };
    if (!base64Data) { done(); return; }
    try {
      const binary = atob(base64Data);
      const bytes = new Uint8Array(binary.length);
      for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i);
      const blob = new Blob([bytes], { type: mimeType || 'audio/mpeg' });
      const url = URL.createObjectURL(blob);

      if (audioPlayer) {
        try { audioPlayer.pause(); } catch (e) { /* ignore */ }
        try { URL.revokeObjectURL(audioPlayer.src); } catch (e) { /* ignore */ }
      }
      audioPlayer = new Audio(url);
      audioPlayer.onloadedmetadata = () => {
        let dur = 0;
        try {
          if (audioPlayer.duration && isFinite(audioPlayer.duration)) dur = audioPlayer.duration;
        } catch (e) { /* ignore */ }
        log('mp3 length loaded: ' + dur.toFixed(2) + 's');
        if (onMetadata) onMetadata(dur);
      };
      audioPlayer.onended = done;
      audioPlayer.onerror = () => { warn('audio playback error'); done(); };
      log('playing mp3 (' + binary.length + ' bytes)');
      audioPlayer.play().catch((e) => { errorLog('audio play() rejected:', e); done(); });
    } catch (e) {
      errorLog('failed to decode audio:', e);
      done();
    }
  }

  // ---- Role / status chip ----------------------------------------------------
  function setRole(name) {
    state.role = name || '';
    updateStatus();
  }

  // ---- Suggestion buttons + auto-continue timer ------------------------------
  async function fetchSuggestions() {
    if (!state.convId) return;
    log('requesting ' + suggestionCount + ' button responses from the LLM (Generating buttons)…');
    try {
      const data = await api('/api/chat/suggestions', {
        method: 'POST',
        body: { conversation_id: state.convId },
      });
      const list = (data && Array.isArray(data.suggestions)) ? data.suggestions : [];
      log('LLM returned ' + list.length + ' buttons');
      if (list.length > 0) {
        renderSuggestionButtons(list);
      } else {
        warn('no buttons returned by the LLM');
      }
    } catch (e) {
      warn('failed to fetch buttons:', e.message);
    }
  }

  function renderSuggestionButtons(list) {
    const container = el.suggestionButtons;
    if (!container) return;
    container.innerHTML = '';
    suggestionButtons = list.slice(0, suggestionCount);
    for (let i = 0; i < suggestionButtons.length; i++) {
      const text = suggestionButtons[i];
      const btn = document.createElement('button');
      btn.type = 'button';
      btn.className = 'suggestion-btn';
      btn.textContent = text;
      btn.title = text;
      btn.addEventListener('click', () => handleSuggestionClick(text));
      container.appendChild(btn);
    }
    container.classList.remove('hidden');
    log('rendered ' + suggestionButtons.length + ' suggestion buttons:', suggestionButtons);
  }

  function clearSuggestions() {
    clearResponseTimer();
    if (el.suggestionButtons && !el.suggestionButtons.classList.contains('hidden')) {
      el.suggestionButtons.classList.add('hidden');
      el.suggestionButtons.innerHTML = '';
      log('buttons disappeared');
    }
    suggestionButtons = [];
  }

  function handleSuggestionClick(text) {
    log('suggestion button clicked:', JSON.stringify(text));
    clearSuggestions();
    clearAutoListenTimer();
    autoStreak = 0;
    void sendText(text, 'button');
  }

  function clearResponseTimer() {
    if (responseTimer) {
      clearTimeout(responseTimer);
      responseTimer = null;
      log('auto-continue timer cancelled');
    }
  }

  function startResponseTimer(seconds) {
    clearResponseTimer();
    const ms = Math.max(0, Number(seconds) || 0) * 1000;
    log('auto-continue timer armed: fires in ' + ms + 'ms (' + seconds + 's, mp3 finished)');
    responseTimer = setTimeout(() => {
      responseTimer = null;
      if (!autoContinueOn) {
        log('auto-continue stopped since the timer was armed — not pushing');
        return;
      }
      if (state.sending || !state.convId) return;
      log('auto-continue timer fired — sending "continue" on your behalf');
      void sendText('the user did not respond, generate a response', 'auto');
    }, ms);
  }

  // ---- Local chat commands (status / delay / stop / start / buttons / text) --
  // These control or display browser-side behaviour (auto-continue "pushing",
  // whether the narration text is shown), so they are handled right here
  // instead of being sent to the server / LLM.
  function handleLocalCommand(raw) {
    const text = String(raw || '').trim();
    const lower = text.toLowerCase();
    const match = lower.match(/^(delay|buttons|listen)(?:\s+(\d+))?$/);
    const textMatch = lower.match(/^text(?:\s+(on|off))?$/);
    const isStatus = lower === 'status';
    if (lower !== 'stop' && lower !== 'start' && !match && !textMatch && !isStatus) return false;

    const notice = (md) => appendMessage('assistant', md, { notice: true });

    // "status" is rendered here (instead of on the server) so it can also show
    // the browser-side delay and buttons settings. It mirrors the help-list
    // style, but without the [n] argument hints — those live in "help" only.
    if (isStatus) {
      appendMessage('user', text);
      const voiceState = voiceOn ? 'ON' : 'OFF (muted)';
      const voiceDesc = voiceLabel || voiceFriendly || voiceName || '—';
      const speedMult = (1 + (voiceSpeed || 3) / 10).toFixed(1);
      notice('`voice` – ' + voiceState + ' — ' + voiceDesc + '\n' +
        '`role` – ' + (state.role || '—') + '\n' +
        '`speed` – ' + voiceSpeed + ' (' + speedMult + 'x)\n' +
        '`delay` – ' + autoDelaySeconds + 's\n' +
        '`buttons` – ' + suggestionCount + '\n' +
        '`listen` – ' + (autoListenOn ? 'ON (mic auto-opens after mp3)' : 'OFF'));
      return true;
    }

    // "text on/off" hides/shows the narration bubbles. The voice reply and the
    // suggestion buttons are unaffected.
    if (textMatch) {
      const want = textMatch[1];
      if (!want) {
        appendMessage('user', text);
        notice('**Text display:** ' + (textOn ? 'ON' : 'OFF') + ' — narration is ' +
          (textOn ? 'shown on screen' : 'voice-only') + '. Toggle with `text on` / `text off`.');
        return true;
      }
      const next = want === 'on';
      if (next === textOn) {
        appendMessage('user', text);
        notice('**Text display** is already ' + (textOn ? 'ON' : 'OFF') + '.');
        return true;
      }
      textOn = next;
      saveLocalStr(KEY_TEXT, textOn ? '1' : '0');
      // Redraw the transcript so the narration hides (text off) or comes back
      // (text on) immediately. The command bubbles are re-added below.
      if (state.conv) renderMessages(state.conv.messages);
      appendMessage('user', text);
      notice(textOn
        ? '**Text display ON.** Replies are shown on screen again (voice and buttons still work).'
        : '**Text display OFF.** Narration is voice-only now — the voice still plays and the buttons still show.');
      return true;
    }

    appendMessage('user', text);   // same look as the server-side commands
    autosizeInput();

    if (lower === 'stop') {
      if (!autoContinueOn && !autoListenOn) {
        notice('Hands-free and auto-continue are already **stopped**.');
      } else {
        autoContinueOn = false;
        autoListenOn = false;
        autoStreak = 0;
        clearResponseTimer();
        clearAutoListenTimer();
        stopMic();
        saveLocalStr(KEY_PUSH, '0');
        saveLocalStr(KEY_LISTEN, '0');
        notice('**Stopped.** No auto-mic, no pushed messages — you stay in control. (`start` resumes.)');
      }
      return true;
    }

    if (lower === 'start') {
      autoContinueOn = true;
      autoListenOn = true;
      autoStreak = 0;
      saveLocalStr(KEY_PUSH, '1');
      saveLocalStr(KEY_LISTEN, '1');
      notice('**Hands-free started.** After each voice reply the mic opens automatically (delay ~1s). (`stop` turns it off.)');
      return true;
    }

    const cmd = match[1];
    const hasValue = match[2] !== undefined;
    const n = hasValue ? parseInt(match[2], 10) : 0;

    if (cmd === 'delay') {
      if (!hasValue) {
        notice('**Push delay:** ' + autoDelaySeconds + 's — the pause after the voice reply before "continue" is pushed. Set with `delay [n]`.');
        return true;
      }
      if (n > MAX_AUTO_DELAY) {
        notice('`delay` takes a number of seconds from 0 to ' + MAX_AUTO_DELAY + '.');
        return true;
      }
      autoDelaySeconds = n;
      saveLocalStr(KEY_DELAY, autoDelaySeconds);
      notice('**Push delay set to ' + autoDelaySeconds + 's.** She now waits that long after each voice reply before pushing "continue".');
      return true;
    }

    if (cmd === 'listen') {
      autoListenOn = !autoListenOn;
      saveLocalStr(KEY_LISTEN, autoListenOn ? '1' : '0');
      if (!autoListenOn) { clearAutoListenTimer(); stopMic(); }
      notice('**Auto-mic ' + (autoListenOn ? 'ON.** The mic opens after each voice reply.' : 'OFF.** Tap MIC to talk manually.'));
      return true;
    }

    // cmd === 'buttons'
    if (!hasValue) {
      notice('**Suggestion buttons:** ' + suggestionCount + ' per reply (1–' + MAX_SUGGESTION_BUTTONS + '). Set with `buttons [n]`.');
      return true;
    }
    if (n < 1 || n > MAX_SUGGESTION_BUTTONS) {
      notice('`buttons` takes a number from 1 to ' + MAX_SUGGESTION_BUTTONS + '.');
      return true;
    }
    suggestionCount = n;
    saveLocalStr(KEY_BUTTONS, suggestionCount);
    notice('**Suggestion buttons set to ' + suggestionCount + '.** Future replies will show ' + suggestionCount + ' button' + (suggestionCount === 1 ? '' : 's') + '.');
    return true;
  }

  // ---- Sending messages ------------------------------------------------------
  async function sendText(text, source) {
    text = String(text || '').trim();
    if (!text || state.sending || !state.convId) {
      warn('send skipped', { text: text, sending: state.sending, hasConv: !!state.convId });
      return;
    }
    if (source === 'auto' && autoStreak >= MAX_AUTO_CONTINUES) {
      warn('auto-continue cap reached (' + autoStreak + ') — waiting for you to type or press a button');
      return;
    }
    if (source !== 'auto') autoStreak = 0;

    clearSuggestions();
    clearAutoListenTimer();
    state.sending = true;
    el.btnSend.disabled = true;
    if (source === 'typed') el.inputMessage.value = '';
    appendMessage('user', text);
    log('POST /api/chat', { user_message: text, source: source || 'typed' });

    try {
      const data = await api('/api/chat', {
        method: 'POST',
        body: { conversation_id: state.convId, user_message: text },
      });
      const updated = data.conversation || data;
      state.conv = updated;
      if (data.role) setRole(data.role);
      if (typeof data.voice === 'boolean' || data.voice_name || data.voice_display || data.voice_speed) {
        applyVoiceState(data);
        if (typeof data.voice === 'boolean') {
          try { localStorage.setItem(KEY_VOICE, voiceOn ? '1' : '0'); } catch (e) { /* ignore */ }
        }
        renderVoiceButton();
      }
      renderMessages(updated.messages);
      if (Array.isArray(data.tool_calls) && data.tool_calls.length > 0) {
        for (const tc of data.tool_calls) {
          let label = tc.name;
          try {
            const a = JSON.parse(tc.arguments || '{}');
            if (a.action) label += ' · ' + a.action;
            if (a.duration_seconds != null) label += ' (' + a.duration_seconds + 's)';
          } catch (e) { /* ignore */ }
          appendMessage('assistant', '🔐 ' + label + ' → ' + (tc.result || ''), { notice: true });
        }
      }
      log('chat reply received. auto =', data.auto);

      if (data.notice) {
        log('server notice:', String(data.notice).split('\n')[0]);
        const shown = (updated.messages || []).some((m) => m.role === 'user' && m.content === text);
        if (!shown && source !== 'auto') appendMessage('user', text);
        appendMessage('assistant', data.notice, { notice: true });
        return;
      }
      if (data.auto) {
        if (source === 'auto') autoStreak += 1;
        handleAutoReply(data);
      } else {
        log('reply is a local/system message — no buttons or timer scheduled');
      }
    } catch (e) {
      errorLog('chat request failed:', e.message);
      appendMessage('assistant', 'Error: ' + e.message, { error: true });
    } finally {
      state.sending = false;
      el.btnSend.disabled = false;
      if (el.inputMessage) el.inputMessage.focus();
    }
  }

  // Called for every real LLM reply. The suggestion buttons are requested
  // immediately (so the person can answer while the mp3 is still playing).
  // The auto-continue timer is armed to fire mp3-length + autoDelaySeconds
  // after the reply arrived, using the loaded mp3 duration when available.
  function handleAutoReply(data) {
    // Ask for the buttons right away — they can be pressed while speaking.
    void fetchSuggestions();

    if (data.audio) {
      log('mp3 received (' + data.audio.length + ' base64 chars)');
      playAudio(data.audio, data.audio_mime || 'audio/mpeg', () => {
        log('mp3 finished playing');
        if (autoListenOn) { scheduleAutoListen(autoListenDelayMs, 'mp3 ended'); return; }
        if (!autoContinueOn) { log('auto-continue stopped — staying quiet'); return; }
        startResponseTimer(autoDelaySeconds);
      }, (dur) => {
        if (!autoListenOn && autoContinueOn) {
          const waitSec = Math.max(0.5, (Math.max(0, Number(dur) || 0) + autoDelaySeconds));
          log('auto-continue: mp3=' + (Number(dur) || 0).toFixed(1) + 's + ' + autoDelaySeconds + 's delay, arming in ' + waitSec.toFixed(1) + 's');
          startResponseTimer(waitSec);
        }
      });
    } else {
      log('no mp3 in reply (voice muted or TTS failed)');
      if (autoListenOn) { scheduleAutoListen(autoListenDelayMs, 'no mp3'); return; }
      if (!autoContinueOn) { log('auto-continue stopped — staying quiet'); return; }
      startResponseTimer(autoDelaySeconds);
    }
  }

  // ---- Voice input (Web Speech API) ----
  // Browser built-in speech recognition (Chrome desktop + Android Chrome).
  // Transcript goes into the composer and sends via normal sendText().
  var micRec = null;
  var micListening = false;
  var micFinal = '';
  function micSupported() { return !!((window.SpeechRecognition || window.webkitSpeechRecognition)); }
  function setMicUI(listening) {
    micListening = listening;
    if (el.btnMic) {
      el.btnMic.classList.toggle('listening', listening);
      el.btnMic.textContent = listening ? 'STOP' : 'MIC';
      el.btnMic.setAttribute('aria-pressed', String(listening));
    }
    if (el.inputMessage) el.inputMessage.placeholder = listening ? 'Listening... speak now (tap STOP when done)' : 'Message Vivid Mistress... (Enter to send, Shift+Enter for new line)';
  }
  function stopMic() { if (micRec && micListening) { try { micRec.stop(); } catch (e) {} } }
  // Turns the MIC button red BEFORE start() so you see feedback even if the
  // browser is slow to fire onstart (or rejects the auto-start entirely).
  function setMicArmed(armed) {
    if (el.btnMic) {
      el.btnMic.classList.toggle('listening', armed || micListening);
      if (armed && !micListening) el.btnMic.textContent = '...';
      else if (!micListening) el.btnMic.textContent = 'MIC';
    }
    if (armed && el.inputMessage) el.inputMessage.placeholder = 'Opening mic...';
  }
  // auto = true when the mic is opened automatically after an mp3 (not a tap).
  // Chrome only allows start() from a user gesture, so an auto-start may be
  // rejected — in that case fall back to the old auto-continue push timer.
  function startMic(auto) {
    if (!micSupported()) { if (!auto) appendMessage('assistant', 'Voice input not supported here. Try Chrome (desktop or Android).', { notice: true }); return false; }
    if (micListening) return true;
    if (!window.isSecureContext) { if (!auto) appendMessage('assistant', 'Mic needs HTTPS (or localhost). Plain http://LAN-IP blocks the mic on Android. Use https:// or http://localhost:port.', { notice: true }); return false; }
    try { if (audioPlayer) audioPlayer.pause(); } catch (e) {}
    clearResponseTimer();
    clearAutoListenTimer();
    var SR = window.SpeechRecognition || window.webkitSpeechRecognition;
    micRec = new SR();
    micRec.lang = 'en-US';
    micRec.interimResults = false;
    micRec.continuous = false;
    micRec.maxAlternatives = 1;
    micFinal = '';
    micRec.onstart = function () { log('mic: listening started' + (auto ? ' (auto)' : '')); setMicUI(true); };
    var autoFailed = false;
    var failAuto = function (code) {
      if (!auto || autoFailed) return;
      autoFailed = true;
      setMicArmed(false);
      warn('auto-mic blocked (' + code + ') — need one manual MIC tap first, falling back to push timer');
      appendMessage('assistant', 'Tap MIC once to enable hands-free (browser blocks auto-mic until then).', { notice: true });
      if (autoContinueOn) startResponseTimer(autoDelaySeconds);
    };
    micRec.onresult = function (event) {
      var t = '';
      try { t = event.results[event.results.length - 1][0].transcript; } catch (e) { t = ''; }
      if (t) {
        micFinal = t;
        if (el.inputMessage) { el.inputMessage.value = t; autosizeInput(); }
        log('mic: transcript', JSON.stringify(t));
      }
    };
    micRec.onerror = function (event) {
      var code = event && event.error;
      warn('mic error:', code);
      setMicArmed(false);
      setMicUI(false);
      if (auto && (code === 'not-allowed' || code === 'service-not-allowed')) { failAuto(code); return; }
      if (code === 'not-allowed' || code === 'service-not-allowed') appendMessage('assistant', 'Mic blocked. In Android Chrome: lock icon in address bar > Permissions > Microphone > Allow, then reload. Needs HTTPS or localhost.', { notice: true });
      else if (code === 'no-speech') {
        if (auto) { log('auto-mic heard nothing — re-arming'); scheduleAutoListen(1500, 'retry after no-speech'); return; }
        appendMessage('assistant', 'Did not hear anything - tap MIC and speak, then pause.', { notice: true });
      }
      else if (code === 'network') appendMessage('assistant', 'Speech service needs internet (Google recogniser). Check connection and try again.', { notice: true });
      else if (code === 'aborted') { log('mic aborted (tap STOP or auto-stop)'); }
      else if (!auto) appendMessage('assistant', 'Mic error: ' + code + '. Tap MIC once and speak, then pause.', { notice: true });
    };
    micRec.onend = function () {
      log('mic: ended, final=', JSON.stringify(micFinal));
      setMicArmed(false);
      setMicUI(false);
      if (autoFailed) return;
      var text = (micFinal || (el.inputMessage ? el.inputMessage.value : '')).trim();
      if (text) {
        if (handleLocalCommand(text)) { el.inputMessage.value = ''; autosizeInput(); return; }
        log('mic: auto-sending', JSON.stringify(text));
        void sendText(text, 'voice');
        if (el.inputMessage) { el.inputMessage.value = ''; autosizeInput(); }
      } else if (auto) {
        log('auto-mic got no text — re-arming once');
        scheduleAutoListen(1500, 'retry after empty');
      }
    };
    try { log('mic: start()' + (auto ? ' (auto)' : ' (tap)')); setMicArmed(true); micRec.start(); }
    catch (e) {
      errorLog('mic start failed:', e);
      setMicArmed(false); setMicUI(false);
      if (auto) failAuto('start-threw'); else appendMessage('assistant', 'Could not start mic: ' + e.message, { notice: true });
      return false;
    }
    // If the browser never fires onstart (gesture-blocked auto-start), the
    // onend with empty text will re-arm / fall back. Also add a watchdog:
    if (auto) setTimeout(function () {
      if (!micListening && !micFinal && !autoFailed) failAuto('no-start');
    }, 2500);
    return true;
  }
  function toggleMic() {
    if (micListening) { stopMic(); return; }
    startMic(false);
  }


  // ---- Composer + events -----------------------------------------------------
  function sendMessage() {
    const text = el.inputMessage.value.trim();
    if (!text) return;
    el.inputMessage.value = '';
    autosizeInput();
    if (handleLocalCommand(text)) return;
    void sendText(text, 'typed');
  }

  function autosizeInput() {
    const ta = el.inputMessage;
    if (!ta) return;
    ta.style.height = 'auto';
    ta.style.height = Math.min(ta.scrollHeight, 150) + 'px';
  }

  if (el.btnSend) el.btnSend.addEventListener('click', sendMessage);
  if (el.btnMic) {
    if (!micSupported()) { el.btnMic.style.opacity = '0.4'; el.btnMic.title = 'Not supported (try Chrome)'; }
    el.btnMic.addEventListener('click', () => {
      // Manual tap always counts as the user gesture Chrome wants — after one
      // tap, auto-starts after mp3s are allowed too.
      if (micListening) { stopMic(); return; }
      clearAutoListenTimer();
      startMic(false);
    });
  }

  if (el.inputMessage) {
    el.inputMessage.addEventListener('keydown', (e) => {
      if (e.key === 'Enter' && !e.shiftKey) {
        e.preventDefault();
        sendMessage();
      }
    });

    // While the user types the auto-continue timer is cancelled — typing means
    // they are about to send, and the reply from that send will re-arm the
    // timer naturally. The suggestion buttons also disappear so they never
    // fight the user.
    el.inputMessage.addEventListener('input', () => {
      autosizeInput();
      clearAutoListenTimer();
      if (responseTimer) {
        log('user is typing — cancelling the auto-continue timer');
        clearResponseTimer();
      }
      if (suggestionButtons.length > 0) {
        log('user is typing — making the buttons disappear');
        clearSuggestions();
      }
    });
  }

  if (el.btnVoice) {
    el.btnVoice.addEventListener('click', async () => {
      try {
        await setVoicePref(!voiceOn);
      } catch (e) {
        alert(e.message);
      }
    });
  }

  autosizeInput();

  // Keep the composer above mobile keyboards that overlay the page instead
  // of resizing the layout viewport. The CSS already has a --keyboard-offset
  // custom property on #input-area; this keeps it in sync on iOS where
  // 100dvh does not shrink when the keyboard opens.
  function initKeyboardAdjustment() {
    if (!window.visualViewport) return;
    const fullViewportHeight = Math.max(window.innerHeight, window.visualViewport.height);
    const updateKeyboardOffset = () => {
      const isMobile = window.matchMedia('(max-width: 768px)').matches;
      const viewport = window.visualViewport;
      const keyboardHeight = fullViewportHeight - viewport.height - viewport.offsetTop;
      const offset = isMobile && keyboardHeight > 100 ? keyboardHeight : 0;
      document.documentElement.style.setProperty('--keyboard-offset', Math.max(0, offset) + 'px');
    };
    window.visualViewport.addEventListener('resize', updateKeyboardOffset);
    window.visualViewport.addEventListener('scroll', updateKeyboardOffset);
    window.addEventListener('resize', updateKeyboardOffset);
    updateKeyboardOffset();
  }

  // ---- Init ----------------------------------------------------------------
  (async function init() {
    loadSessionPrefs();
    initKeyboardAdjustment();
    log('Vivid Mistress loading…');
    try {
      const data = await api('/api/session');
      state.conv = data.conversation;
      state.convId = state.conv.id;
      if (data.role) setRole(data.role);
      renderMessages(state.conv.messages);
      await loadVoice();
      if (el.inputMessage) el.inputMessage.focus();
      log('session ready: conversation=' + state.convId + ' messages=' + ((state.conv.messages || []).length));
    } catch (e) {
      errorLog('init failed — is the server running?', e.message);
      appendMessage('assistant', 'Could not start a session: ' + e.message, { error: true });
    }
  })();
})();





