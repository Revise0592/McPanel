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

  // Forms or buttons with data-confirm ask first.
  document.addEventListener("submit", function (e) {
    var msg = e.target.getAttribute("data-confirm");
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

  // New server form: hide Java-only fields for Bedrock.
  var typeField = document.querySelector("[data-type-picker]");
  if (typeField) {
    var sync = function () {
      var checked = typeField.querySelector("input:checked");
      var bedrock = checked && checked.value === "bedrock";
      document.querySelectorAll("[data-java-only]").forEach(function (el) { el.hidden = bedrock; });
      var port = document.querySelector("input[name=port]");
      if (port) port.placeholder = bedrock ? "Automatic (19132+)" : "Automatic (25565+)";
    };
    typeField.addEventListener("change", sync);
    sync();
  }

  // Live console.
  var box = document.getElementById("console");
  if (box) {
    var maxLines = 2000;
    var classify = function (line) {
      if (line.indexOf("[McPanel] > ") === 0) return "cmd";
      if (line.indexOf("[McPanel]") === 0) return "panel";
      if (/\b(ERROR|SEVERE|Exception)\b/.test(line)) return "err";
      if (/\bWARN(ING)?\b/.test(line)) return "warn";
      return "";
    };
    var nearBottom = function () { return box.scrollHeight - box.scrollTop - box.clientHeight < 40; };
    var add = function (line) {
      var stick = nearBottom();
      var div = document.createElement("div");
      var c = classify(line);
      if (c) div.className = c;
      div.textContent = line;
      box.appendChild(div);
      while (box.childNodes.length > maxLines) box.removeChild(box.firstChild);
      if (stick) box.scrollTop = box.scrollHeight;
    };
    var connect = function () {
      box.textContent = "";
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
    var history = [], pos = 0;
    form.addEventListener("submit", function (e) {
      e.preventDefault();
      var cmd = input.value.trim();
      if (!cmd) return;
      history.push(cmd);
      pos = history.length;
      input.value = "";
      fetch(form.action, {
        method: "POST",
        headers: { "Content-Type": "application/x-www-form-urlencoded" },
        body: "command=" + encodeURIComponent(cmd),
        credentials: "same-origin"
      }).then(function (r) {
        if (!r.ok) return r.text().then(function (t) { toast(t.trim()); });
      });
      box.scrollTop = box.scrollHeight;
    });
    input.addEventListener("keydown", function (e) {
      if (e.key === "ArrowUp" && pos > 0) { pos--; input.value = history[pos]; e.preventDefault(); }
      if (e.key === "ArrowDown") {
        if (pos < history.length - 1) { pos++; input.value = history[pos]; } else { pos = history.length; input.value = ""; }
        e.preventDefault();
      }
    });
  }

  // Copy buttons.
  document.addEventListener("click", function (e) {
    var b = e.target.closest("[data-copy]");
    if (!b) return;
    navigator.clipboard && navigator.clipboard.writeText(b.getAttribute("data-copy")).then(function () {
      var old = b.textContent;
      b.textContent = "Copied";
      setTimeout(function () { b.textContent = old; }, 1200);
    });
  });
})();
