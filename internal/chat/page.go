package chat

// pageHTML is the page of the chat window. {{nonce}} is replaced by a new
// random value for each response, and only the script and the style with that
// value may run (see the Content-Security-Policy in server.go).
//
// The messages come from strangers. The script only ever puts them on the page
// as text (textContent), never as HTML.
const pageHTML = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="color-scheme" content="dark light">
<title>Chat replay</title>
<style nonce="{{nonce}}">
:root { color-scheme: dark light; --bg:#0f0f10; --fg:#e8e8ea; --muted:#8a8a92; --author:#7cb7ff; --paid:#ffd166; --member:#6ee7a8; --line:#26262a; }
@media (prefers-color-scheme: light) { :root { --bg:#ffffff; --fg:#1b1b1f; --muted:#6b6b75; --author:#1a5fb4; --paid:#9a6700; --member:#1a7f4b; --line:#e4e4e8; } }
* { box-sizing: border-box; }
html, body { height: 100%; margin: 0; }
body { display: flex; flex-direction: column; background: var(--bg); color: var(--fg); font: 14px/1.4 system-ui, sans-serif; }
header { padding: 8px 12px; border-bottom: 1px solid var(--line); display: flex; gap: 10px; align-items: baseline; }
header b { font-size: 12px; letter-spacing: .08em; }
#title { color: var(--muted); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; flex: 1; }
#clock { color: var(--muted); font-variant-numeric: tabular-nums; }
#list { flex: 1; overflow-y: auto; padding: 6px 12px; }
.msg { padding: 2px 0; overflow-wrap: anywhere; }
.t { color: var(--muted); font-size: 12px; margin-right: 6px; font-variant-numeric: tabular-nums; }
.a { color: var(--author); font-weight: 600; margin-right: 4px; }
.k1 .a, .k1 .x { color: var(--paid); }
.k2 .a, .k2 .x { color: var(--member); }
#status { padding: 6px 12px; color: var(--muted); border-top: 1px solid var(--line); min-height: 30px; }
</style>
</head>
<body>
<header><b>CHAT REPLAY</b><span id="title"></span><span id="clock"></span></header>
<div id="list"></div>
<div id="status">Loading...</div>
<script nonce="{{nonce}}">
(function () {
  "use strict";
  var list = document.getElementById("list");
  var statusEl = document.getElementById("status");
  var titleEl = document.getElementById("title");
  var clockEl = document.getElementById("clock");
  var busy = false;
  var lastKey = "";

  function stamp(seconds) {
    var s = Math.floor(seconds);
    var h = Math.floor(s / 3600), m = Math.floor((s % 3600) / 60), r = s % 60;
    var two = function (n) { return (n < 10 ? "0" : "") + n; };
    return h > 0 ? h + ":" + two(m) + ":" + two(r) : m + ":" + two(r);
  }

  function row(message) {
    var div = document.createElement("div");
    div.className = "msg k" + message.k;
    var t = document.createElement("span");
    t.className = "t";
    t.textContent = stamp(message.t);
    var a = document.createElement("span");
    a.className = "a";
    a.textContent = message.a + (message.k === 2 ? "" : ":");
    var x = document.createElement("span");
    x.className = "x";
    x.textContent = message.x;
    div.append(t, a, x);
    return div;
  }

  function render(data) {
    document.title = "Chat replay - " + data.title;
    titleEl.textContent = data.title;
    clockEl.textContent = stamp(data.time);
    statusEl.textContent = data.status;

    var last = data.messages.length ? data.messages[data.messages.length - 1] : null;
    var key = data.messages.length + ":" + (last ? last.t + last.a : "") + ":" + (data.messages.length ? data.messages[0].t : "");
    if (key === lastKey) return;
    lastKey = key;

    var atBottom = list.scrollHeight - list.scrollTop - list.clientHeight < 40;
    list.replaceChildren.apply(list, data.messages.map(row));
    if (atBottom) list.scrollTop = list.scrollHeight;
  }

  function poll() {
    if (busy) return;
    busy = true;
    fetch("messages", { cache: "no-store" })
      .then(function (r) { if (!r.ok) throw new Error(r.status); return r.json(); })
      .then(render)
      .catch(function () { statusEl.textContent = "Lost the connection to ytwatch."; })
      .then(function () { busy = false; });
  }

  poll();
  setInterval(poll, 700);
})();
</script>
</body>
</html>
`
