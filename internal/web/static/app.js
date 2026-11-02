// McPanel front-end helpers: live console, confirmations, small toggles.
(function () {
  "use strict";

  function toast(msg) {
    var t = document.getElementById("toast");
    if (!t) return;
    t.textContent = msg;
    t.hidden = false;
    clearTimeout(t._timer);
    t._timer = setTimeout(function () { t.hidden = true; }, 5000);
  }

  // Errors sent by the server via HX-Trigger.
  document.body.addEventListener("showError", function (e) { toast(e.detail.value || e.detail); });
  document.body.addEventListener("htmx:responseError", function (e) {
    if (e.detail.xhr.status !== 401) toast(e.detail.xhr.responseText || "Something went wrong.");
  });

  // Pages marked data-reload-after-task reload when the server's job (a
  // backup, an install) finishes, so their lists show the result.
  var header = document.querySelector(".server-header[data-task]");
  var lastTask = header ? header.getAttribute("data-task") : "";
  document.body.addEventListener("htmx:afterSettle", function () {
    var h = document.querySelector(".server-header[data-task]");
    if (!h) return;
    var task = h.getAttribute("data-task");
    if (lastTask && !task && document.querySelector("[data-reload-after-task]")) location.reload();
    lastTask = task;
  });

  // Usage charts: pointing at any chart moves a line across all of them to
  // the nearest sample and shows its values above.
  function hideUsage(box) {
    box.querySelectorAll(".crosshair").forEach(function (c) { c.hidden = true; });
    var out = box.parentNode.querySelector(".usage-readout");
    if (out) { out.textContent = out.getAttribute("data-default"); out.classList.remove("live"); }
  }
  document.addEventListener("pointermove", function (e) {
    var area = e.target.closest && e.target.closest(".plot-area");
    var box = area && area.closest(".usage-charts");
    if (!box) return;
    if (!box._points) box._points = JSON.parse(box.getAttribute("data-points") || "[]");
    var start = +box.getAttribute("data-start"), end = +box.getAttribute("data-end");
    var r = area.getBoundingClientRect();
    var t = start + (e.clientX - r.left) / r.width * (end - start);
    var best = null;
    box._points.forEach(function (p) { if (!best || Math.abs(p[0] - t) < Math.abs(best[0] - t)) best = p; });
    if (!best || Math.abs(best[0] - t) > 2 * +box.getAttribute("data-bucket")) { hideUsage(box); return; }
    var left = (best[0] - start) / (end - start) * 100 + "%";
    box.querySelectorAll(".crosshair").forEach(function (c) { c.style.left = left; c.hidden = false; });
    var d = new Date(best[0]);
    var text = d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" }) +
      "  ·  CPU " + Math.round(best[1]) + "%  ·  Memory " + (best[2] < 1024 ? Math.round(best[2]) + " MB" : (best[2] / 1024).toFixed(1) + " GB");
    if (best[3] !== null) text += "  ·  TPS " + best[3].toFixed(1);
    var out = box.parentNode.querySelector(".usage-readout");
    if (out) { out.textContent = text; out.classList.add("live"); }
  });
  document.addEventListener("pointerout", function (e) {
    var area = e.target.closest && e.target.closest(".plot-area");
    if (area && !area.contains(e.relatedTarget)) hideUsage(area.closest(".usage-charts"));
  });

  // Forms or buttons with data-confirm ask first.
  document.addEventListener("submit", function (e) {
    var msg = (e.submitter && e.submitter.getAttribute("data-confirm")) || e.target.getAttribute("data-confirm");
    if (msg && !window.confirm(msg)) e.preventDefault();
  }, true);

  // Forms with data-busy show progress text on their submit button, since
  // uploads can take a while.
  document.addEventListener("submit", function (e) {
    var busy = e.target.getAttribute("data-busy");
    if (!busy || e.defaultPrevented) return;
    var b = e.submitter || e.target.querySelector("button[type=submit], button:not([type])");
    if (b && b.hasAttribute("formnovalidate")) return;
    if (b) { b.disabled = true; b.textContent = busy; }
  });
  document.body.addEventListener("htmx:confirm", function (e) {
    var msg = e.detail.elt.getAttribute("data-confirm");
    if (!msg) return;
    e.preventDefault();
    if (window.confirm(msg)) e.detail.issueRequest(true);
  });

  // New server form: hide Java-only fields for Bedrock, and world fields
  // for the Velocity proxy. Hidden fields are disabled so they don't block
  // sending the form.
  var typeField = document.querySelector("[data-type-picker]");
  if (typeField) {
    var lastType = "";
    var show = function (sel, on) {
      document.querySelectorAll(sel).forEach(function (el) {
        el.hidden = !on;
        el.querySelectorAll("input, select").forEach(function (i) { i.disabled = !on; });
      });
    };
    var sync = function () {
      var checked = typeField.querySelector("input:checked");
      var type = checked ? checked.value : "";
      var bedrock = type === "bedrock", proxy = type === "velocity";
      show("[data-java-only]", !bedrock);
      show("[data-world-only]", !proxy);
      show("[data-proxy-only]", proxy);
      var port = document.querySelector("input[name=port]");
      if (port) port.placeholder = bedrock ? "Automatic (19132+)" : "Automatic (25565+)";
      var mem = document.querySelector("select[name=memory]");
      if (mem && type !== lastType && (proxy || lastType === "velocity")) mem.value = proxy ? "512" : "2048";
      lastType = type;
    };
    typeField.addEventListener("change", sync);
    sync();
  }

  // Fields marked data-show-if="name=value" only show while that field of
  // their form has that value.
  document.querySelectorAll("[data-show-if-form]").forEach(function (form) {
    var deps = form.querySelectorAll("[data-show-if]");
    var sync = function () {
      deps.forEach(function (el) {
        var kv = el.getAttribute("data-show-if").split("=");
        var field = form.elements[kv[0]];
        el.hidden = !field || field.value !== kv[1];
      });
    };
    form.addEventListener("change", sync);
    sync();
  });

  // Live console.
  var box = document.getElementById("console");
  if (box) {
    var maxLines = 2000;
    var lastClass = "";
    var classify = function (line) {
      // Stack traces and other follow-on lines take the class of the line
      // they belong to, so "Only problems" shows them whole.
      if ((lastClass === "err" || lastClass === "warn") && /^(\s|Caused by:|\.\.\. \d+ more)/.test(line)) return lastClass;
      if (line.indexOf("[McPanel] > ") === 0) return "cmd";
      if (line.indexOf("[McPanel]") === 0) return "panel";
      if (/\b(ERROR|SEVERE|Exception)\b/.test(line)) return "err";
      if (/\bWARN(ING)?\b/.test(line)) return "warn";
      if (/\]: (\[Not Secure\] )?<[^>\s]+> /.test(line)) return "chat";
      return "";
    };
    var nearBottom = function () { return box.scrollHeight - box.scrollTop - box.clientHeight < 40; };
    var add = function (line) {
      var stick = nearBottom();
      var div = document.createElement("div");
      var c = classify(line);
      lastClass = c;
      if (c) div.className = c;
      div.textContent = line;
      box.appendChild(div);
      while (box.childNodes.length > maxLines) box.removeChild(box.firstChild);
      if (stick) box.scrollTop = box.scrollHeight;
    };
    // Filter buttons hide chat, or everything but warnings and errors. The
    // choice is remembered in this browser.
    var filterKey = "mcpanel-console-filter";
    var setFilter = function (f) {
      box.setAttribute("data-filter", f);
      document.querySelectorAll("[data-filter]").forEach(function (b) {
        if (b !== box) b.setAttribute("aria-pressed", b.getAttribute("data-filter") === f);
      });
      box.scrollTop = box.scrollHeight;
    };
    document.querySelectorAll("button[data-filter]").forEach(function (b) {
      b.addEventListener("click", function () {
        var f = b.getAttribute("data-filter");
        try { localStorage.setItem(filterKey, f); } catch (err) {}
        setFilter(f);
      });
    });
    try { setFilter(localStorage.getItem(filterKey) || ""); } catch (err) { setFilter(""); }

    var connect = function () {
      box.textContent = "";
      lastClass = "";
      var es = new EventSource(box.getAttribute("data-stream"));
      es.onmessage = function (e) { add(e.data); };
      es.onerror = function () {
        es.close();
        setTimeout(connect, 3000);
      };
    };
    connect();

    var form = document.getElementById("command-form");
    var input = form.querySelector("input");
    // Earlier commands are kept per server in this browser.
    var historyKey = "mcpanel-history:" + form.getAttribute("action");
    var history = [];
    try { history = JSON.parse(localStorage.getItem(historyKey) || "[]"); } catch (err) {}
    if (!Array.isArray(history)) history = [];
    var pos = history.length;
    var send = function (cmd) {
      fetch(form.action, {
        method: "POST",
        headers: { "Content-Type": "application/x-www-form-urlencoded" },
        body: "command=" + encodeURIComponent(cmd),
        credentials: "same-origin"
      }).then(function (r) {
        if (!r.ok) return r.text().then(function (t) { toast(t.trim()); });
      });
      box.scrollTop = box.scrollHeight;
    };
    form.addEventListener("submit", function (e) {
      e.preventDefault();
      var cmd = input.value.trim();
      if (!cmd) return;
      if (history[history.length - 1] !== cmd) history.push(cmd);
      if (history.length > 50) history = history.slice(-50);
      try { localStorage.setItem(historyKey, JSON.stringify(history)); } catch (err) {}
      pos = history.length;
      input.value = "";
      send(cmd);
    });
    // Quick buttons either send a command straight away or start one in
    // the box for the rest to be typed.
    document.querySelectorAll("[data-cmd]").forEach(function (b) {
      b.addEventListener("click", function () { send(b.getAttribute("data-cmd")); });
    });
    document.querySelectorAll("[data-fill]").forEach(function (b) {
      b.addEventListener("click", function () {
        input.value = b.getAttribute("data-fill");
        input.focus();
      });
    });
    // Tab completes the last word to the name of a player who's online
    // (from the Players online card); pressing it again picks the next one.
    var cycle = null;
    var complete = function () {
      if (!cycle) {
        var word = input.value.match(/\S*$/)[0];
        if (!word) return false;
        var lower = word.toLowerCase();
        var names = [];
        document.querySelectorAll("[data-player]").forEach(function (el) {
          var n = el.getAttribute("data-player");
          if (n.toLowerCase().indexOf(lower) === 0) names.push(n);
        });
        if (!names.length) return false;
        cycle = { before: input.value.slice(0, input.value.length - word.length), names: names, i: 0 };
      } else {
        cycle.i = (cycle.i + 1) % cycle.names.length;
      }
      input.value = cycle.before + cycle.names[cycle.i];
      return true;
    };
    input.addEventListener("input", function () { cycle = null; });
    input.addEventListener("keydown", function (e) {
      if (e.key === "Tab" && !e.shiftKey && !e.ctrlKey && !e.altKey && !e.metaKey) {
        if (complete()) e.preventDefault();
        return;
      }
      cycle = null;
      if (e.key === "ArrowUp" && pos > 0) { pos--; input.value = history[pos]; e.preventDefault(); }
      if (e.key === "ArrowDown") {
        if (pos < history.length - 1) { pos++; input.value = history[pos]; } else { pos = history.length; input.value = ""; }
        e.preventDefault();
      }
    });
  }

  // File editor: Tab indents with spaces (YAML doesn't allow tabs), Ctrl+S
  // saves, and leaving with unsaved changes asks first.
  var editor = document.querySelector("[data-editor]");
  if (editor) {
    var saved = editor.value, leaving = false;
    editor.addEventListener("keydown", function (e) {
      if (e.key === "Tab" && !e.shiftKey && !e.ctrlKey && !e.altKey && !e.metaKey) {
        e.preventDefault();
        document.execCommand("insertText", false, "  ") || editor.setRangeText("  ", editor.selectionStart, editor.selectionEnd, "end");
      }
      if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "s") {
        e.preventDefault();
        editor.form.requestSubmit();
      }
    });
    editor.form.addEventListener("submit", function () { leaving = true; });
    window.addEventListener("beforeunload", function (e) {
      if (!leaving && editor.value !== saved) e.preventDefault();
    });
  }

  // Accent colour: swatches fill the picker, and the page previews it live.
  var accent = document.getElementById("accent");
  if (accent) {
    var shown = document.querySelector("[data-accent-value]");
    var swatches = document.querySelectorAll("[data-accent]");
    var preview = function () {
      var v = accent.value.toLowerCase();
      var n = parseInt(v.slice(1), 16);
      // Same brightness rule as accentText in brand.go.
      var light = Math.floor(((n >> 16) * 299 + ((n >> 8) & 255) * 587 + (n & 255) * 114) / 1000) > 128;
      var root = document.documentElement.style;
      root.setProperty("--accent", v);
      root.setProperty("--accent-text", light ? "#16191d" : "#fff");
      if (shown) shown.textContent = v;
      swatches.forEach(function (s) { s.setAttribute("aria-pressed", s.getAttribute("data-accent") === v); });
    };
    swatches.forEach(function (s) {
      s.addEventListener("click", function () { accent.value = s.getAttribute("data-accent"); preview(); });
    });
    accent.addEventListener("input", preview);
    preview();
  }

  // Copy buttons. navigator.clipboard only exists on HTTPS or localhost, so
  // fall back to a hidden textarea + execCommand when the panel is on plain HTTP.
  function copyFallback(text) {
    var ta = document.createElement("textarea");
    ta.value = text;
    ta.setAttribute("readonly", "");
    ta.style.position = "fixed";
    ta.style.opacity = "0";
    document.body.appendChild(ta);
    ta.select();
    var ok = false;
    try { ok = document.execCommand("copy"); } catch (err) {}
    document.body.removeChild(ta);
    return ok ? Promise.resolve() : Promise.reject();
  }
  document.addEventListener("click", function (e) {
    var b = e.target.closest("[data-copy]");
    if (!b) return;
    var text = b.getAttribute("data-copy");
    var p = navigator.clipboard && window.isSecureContext
      ? navigator.clipboard.writeText(text).catch(function () { return copyFallback(text); })
      : copyFallback(text);
    p.then(function () {
      var old = b.textContent;
      b.textContent = "Copied";
      setTimeout(function () { b.textContent = old; }, 1200);
    }, function () {
      toast("Couldn't copy. Select the text and copy it by hand.");
    });
  });
  // MOTD editor: draws the description as the multiplayer list would,
  // reading & (or §) codes as the player types.
  var MOTD_GREY = "#AAAAAA";
  var MOTD_SCRAMBLE = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789#$%&?";
  var still = window.matchMedia && window.matchMedia("(prefers-reduced-motion: reduce)").matches;
  function motdShadow(hex) {
    var n = parseInt(hex.slice(1), 16);
    var r = (n >> 16 & 255) >> 2, g = (n >> 8 & 255) >> 2, b = (n & 255) >> 2;
    return "2px 2px 0 rgb(" + r + "," + g + "," + b + ")";
  }
  function scramble(len) {
    var out = "";
    for (var i = 0; i < len; i++) out += MOTD_SCRAMBLE.charAt(Math.floor(Math.random() * MOTD_SCRAMBLE.length));
    return out;
  }
  function renderMotd(box) {
    var input = box.querySelector(".motd-input");
    var out = box.querySelector(".motd-lines");
    var warn = box.querySelector(".motd-warn");
    var colours = {};
    box.querySelectorAll(".motd-swatch").forEach(function (b) { colours[b.getAttribute("data-code")] = b.getAttribute("data-hex"); });
    var lines = input.value.replace(/\r\n?/g, "\n").split("\n");
    out.textContent = "";
    var st = { colour: MOTD_GREY, l: false, o: false, n: false, m: false, k: false };
    lines.slice(0, 2).forEach(function (line) {
      var div = document.createElement("div");
      div.className = "motd-line";
      var buf = "";
      function emit() {
        if (!buf) return;
        var span = document.createElement("span");
        span.style.color = st.colour;
        span.style.textShadow = motdShadow(st.colour);
        if (st.l) span.style.fontWeight = "700";
        if (st.o) span.style.fontStyle = "italic";
        var deco = (st.n ? "underline " : "") + (st.m ? "line-through" : "");
        if (deco) span.style.textDecoration = deco;
        if (st.k) {
          span.className = "motd-k";
          span.setAttribute("data-len", buf.length);
          span.textContent = scramble(buf.length);
        } else {
          span.textContent = buf;
        }
        div.appendChild(span);
        buf = "";
      }
      for (var i = 0; i < line.length; i++) {
        var ch = line.charAt(i), code = line.charAt(i + 1).toLowerCase();
        if ((ch === "&" || ch === "§") && /[0-9a-fk-or]/.test(code)) {
          emit();
          if (colours[code]) st = { colour: colours[code], l: false, o: false, n: false, m: false, k: false };
          else if (code === "r") st = { colour: MOTD_GREY, l: false, o: false, n: false, m: false, k: false };
          else st[code] = true;
          i++;
          continue;
        }
        buf += ch;
      }
      emit();
      if (!div.firstChild) div.appendChild(document.createTextNode(" "));
      out.appendChild(div);
    });
    if (warn) {
      var extra = lines.length > 2 && lines.slice(2).join("").trim() !== "";
      warn.hidden = !extra;
      warn.textContent = extra ? "Only two lines fit in the multiplayer list. Anything after the second line won't be saved." : "";
    }
    var max = document.getElementById("f-max-players");
    var count = box.querySelector(".motd-count");
    if (count) count.textContent = "0/" + (max && max.value !== "" ? max.value : "20");
  }
  var scrambling = 0;
  function setupMotd() {
    document.querySelectorAll(".motd-editor").forEach(function (box) {
      if (box._motd) return;
      box._motd = true;
      renderMotd(box);
      // Scrambled text keeps changing, as it does in the game.
      if (!still && !scrambling) {
        scrambling = setInterval(function () {
          document.querySelectorAll(".motd-k").forEach(function (s) { s.textContent = scramble(+s.getAttribute("data-len")); });
        }, 80);
      }
    });
  }
  setupMotd();
  document.body.addEventListener("htmx:afterSettle", setupMotd);
  document.addEventListener("input", function (e) {
    var box = e.target.closest && e.target.closest(".motd-editor");
    if (box) renderMotd(box);
    else if (e.target.id === "f-max-players") document.querySelectorAll(".motd-editor").forEach(renderMotd);
  });
  document.addEventListener("click", function (e) {
    var b = e.target.closest && e.target.closest(".motd-tools [data-code]");
    if (!b) return;
    var box = b.closest(".motd-editor");
    var input = box.querySelector(".motd-input");
    var code = "&" + b.getAttribute("data-code");
    var start = input.selectionStart, end = input.selectionEnd, v = input.value;
    // With text selected, the code goes before it and a reset after, so
    // only the selection changes.
    var after = end > start && code !== "&r" ? "&r" : "";
    input.value = v.slice(0, start) + code + v.slice(start, end) + after + v.slice(end);
    input.focus();
    var pos = end + code.length + after.length;
    input.setSelectionRange(pos, pos);
    renderMotd(box);
  });
})();
