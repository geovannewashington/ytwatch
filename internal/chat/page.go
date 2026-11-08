package chat

// pageHTML is the page of the chat window. {{nonce}} is replaced by a new
// random value for each response, and only the script and the style with that
// value may run (see the Content-Security-Policy in server.go).
//
// The messages come from strangers. The script only ever puts them on the page
// as text (textContent) or as the address of a picture, never as HTML, and the
// policy lets the browser load pictures from YouTube's image servers only.
//
// The page keeps its own clock. The server gives the time of mpv and the
// messages of the next moments, and the page shows each message when its clock
// reaches the time of the message, so that they appear one by one as they did
// in the live chat. A clock that jumps means a seek, and the chat starts again.
const pageHTML = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="color-scheme" content="dark light">
<meta name="referrer" content="no-referrer">
<title>Chat replay</title>
<style nonce="{{nonce}}">
:root {
  color-scheme: dark light;
  --bg:#0f0f0f; --fg:#f1f1f1; --muted:#9a9aa2; --line:#272727; --card:#1b1b1d;
  --mod:#5e84f1; --member:#2ba640; --owner-bg:#ffd600; --owner-fg:#111; --verified:#aaa;
  --paid-h:#ffb300; --paid-b:#ffca28; --paid-fg:#000000de;
  --member-h:#0f9d58; --gift-h:#0f9d58; --pill:#3ea6ff;
}
@media (prefers-color-scheme: light) {
  :root { --bg:#fff; --fg:#0f0f0f; --muted:#606060; --line:#e5e5e5; --card:#f2f2f2; --mod:#2a5bd7; --member:#1e7e34; --verified:#606060; --pill:#065fd4; }
}
* { box-sizing: border-box; }
html, body { height: 100%; margin: 0; }
body { display: flex; flex-direction: column; background: var(--bg); color: var(--fg); font: 13px/1.45 Roboto, "Noto Sans", system-ui, sans-serif; }
header { padding: 10px 12px; border-bottom: 1px solid var(--line); display: flex; gap: 10px; align-items: baseline; }
header b { font-size: 12px; letter-spacing: .08em; }
#title { color: var(--muted); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; flex: 1; }
#clock { color: var(--muted); font-variant-numeric: tabular-nums; }
#wrap { position: relative; flex: 1; min-height: 0; }
#list { position: absolute; inset: 0; overflow-y: auto; padding: 8px 12px; display: flex; flex-direction: column; scroll-behavior: smooth; overscroll-behavior: contain; }
#list::before { content: ""; margin-top: auto; }
#list.jump { scroll-behavior: auto; }
.msg { display: flex; gap: 10px; align-items: flex-start; padding: 4px 0; overflow-wrap: anywhere; }
.msg > .body { min-width: 0; }
.av { flex: none; width: 24px; height: 24px; border-radius: 50%; background: var(--card); color: var(--muted); font-size: 11px; font-weight: 600; display: grid; place-items: center; object-fit: cover; margin-top: 1px; overflow: hidden; text-transform: uppercase; }
.t { color: var(--muted); font-size: 11px; margin-right: 6px; font-variant-numeric: tabular-nums; }
.name { font-weight: 500; color: var(--muted); margin-right: 6px; }
.name.member { color: var(--member); }
.name.moderator { color: var(--mod); }
.name.owner { background: var(--owner-bg); color: var(--owner-fg); border-radius: 2px; padding: 0 4px; }
.badge { display: inline-block; vertical-align: -3px; margin-right: 4px; }
.badge img, .badge svg { width: 16px; height: 16px; display: block; }
.badge svg { fill: currentColor; }
.badge.moderator { color: var(--mod); }
.badge.verified { color: var(--verified); }
.badge.owner { color: var(--owner-bg); }
.name.owner .badge { color: var(--owner-fg); }
.emoji { width: 20px; height: 20px; vertical-align: -5px; margin: 0 1px; }
.card { margin: 6px 0; border-radius: 8px; overflow: hidden; background: var(--card); overflow-wrap: anywhere; }
.card .head { display: flex; gap: 10px; align-items: center; padding: 8px 10px; }
.card .head .who { min-width: 0; flex: 1; }
.card .head .amount { font-weight: 600; }
.card .head .sub { opacity: .85; font-size: 12px; }
.card .text { padding: 8px 10px; }
.card.paid .head { background: var(--paid-h); color: var(--paid-fg); }
.card.paid .text { background: var(--paid-b); color: var(--paid-fg); }
.card.paid .name { color: inherit; font-weight: 600; margin: 0; }
.card.member .head, .card.gift .head { background: var(--member-h); color: #fff; }
.card.member .name, .card.gift .name { color: #fff; margin: 0; font-weight: 600; }
.card.member .text { background: var(--card); }
.enter { animation: enter .28s cubic-bezier(.2,.8,.2,1) both; }
@keyframes enter { from { opacity: 0; transform: translateY(10px) scale(.98); } to { opacity: 1; transform: none; } }
.card.enter { animation-name: pop; animation-duration: .4s; }
@keyframes pop { from { opacity: 0; transform: translateY(12px) scale(.96); } 60% { transform: translateY(-2px) scale(1.01); } to { opacity: 1; transform: none; } }
@media (prefers-reduced-motion: reduce) { .enter, .card.enter { animation: none; } #list { scroll-behavior: auto; } }
#more { position: absolute; left: 50%; bottom: 10px; transform: translateX(-50%); background: var(--pill); color: #fff; border: 0; border-radius: 16px; padding: 5px 14px; font: inherit; font-size: 12px; cursor: pointer; box-shadow: 0 2px 8px #0006; display: none; }
#more.on { display: block; animation: enter .2s both; }
#status { padding: 6px 12px; color: var(--muted); border-top: 1px solid var(--line); min-height: 30px; }
#status:empty { display: none; }
</style>
</head>
<body>
<header><b>CHAT REPLAY</b><span id="title"></span><span id="clock"></span></header>
<div id="wrap"><div id="list"></div><button id="more" type="button">New messages</button></div>
<div id="status">Loading...</div>
<script nonce="{{nonce}}">
(function () {
  "use strict";

  var SEEK = 3;          // seconds: a jump this big in the clock is a seek
  var MAX_ROWS = 400;    // rows kept on the page
  var ANIMATED = 8;      // rows of one batch that animate, the rest are placed at once
  var list = document.getElementById("list");
  var more = document.getElementById("more");
  var statusEl = document.getElementById("status");
  var titleEl = document.getElementById("title");
  var clockEl = document.getElementById("clock");

  var seen = Object.create(null);   // ids that are on the page or waiting for their time
  var pending = [];                 // messages waiting for their time, by time
  var clock = { known: false, pos: 0, at: 0, playing: false, last: -1 };
  var busy = false;
  var stuck = true;                 // follow the newest message until the user scrolls up
  var autoUntil = 0;                // our own scrolling, which the scroll handler must not take for the user's

  function now() { return performance.now() / 1000; }
  function clockNow() { return clock.playing ? clock.pos + (now() - clock.at) : clock.pos; }

  function stamp(seconds) {
    var s = Math.max(0, Math.floor(seconds));
    var h = Math.floor(s / 3600), m = Math.floor((s % 3600) / 60), r = s % 60;
    var two = function (n) { return (n < 10 ? "0" : "") + n; };
    return h > 0 ? h + ":" + two(m) + ":" + two(r) : m + ":" + two(r);
  }

  function el(tag, cls, text) {
    var e = document.createElement(tag);
    if (cls) e.className = cls;
    if (text !== undefined) e.textContent = text;
    return e;
  }

  function picture(src, cls, alt) {
    var img = el("img", cls);
    img.alt = alt || "";
    img.referrerPolicy = "no-referrer";
    img.decoding = "async";
    img.onerror = function () { img.style.visibility = "hidden"; };
    img.src = src;
    return img;
  }

  function avatar(m) {
    if (m.p) return picture(m.p, "av", "");
    var letter = (m.a || "").replace(/^@/, "").charAt(0);
    return el("span", "av", letter);
  }

  var SVG = "http://www.w3.org/2000/svg";
  var ICONS = {
    moderator: "M22.7 19l-9.1-9.1c.9-2.3.4-5-1.5-6.9-2-2-5-2.4-7.4-1.3L9 6 6 9 1.6 4.7C.4 7.1.9 10.1 2.9 12.1c1.9 1.9 4.6 2.4 6.9 1.5l9.1 9.1c.4.4 1 .4 1.4 0l2.3-2.3c.5-.4.5-1.1.1-1.4z",
    verified: "M12 2a10 10 0 100 20 10 10 0 000-20zm-2 15l-5-5 1.4-1.4L10 14.2l7.6-7.6L19 8l-9 9z",
    owner: "M5 16L3 5l5.5 5L12 4l3.5 6L21 5l-2 11H5zm14 3c0 .6-.4 1-1 1H6c-.6 0-1-.4-1-1v-1h14v1z"
  };

  function badge(b) {
    var span = el("span", "badge " + b.k);
    span.title = b.l || b.k;
    if (b.i) {
      span.appendChild(picture(b.i, "", b.l || ""));
    } else if (ICONS[b.k]) {
      var svg = document.createElementNS(SVG, "svg");
      svg.setAttribute("viewBox", "0 0 24 24");
      var path = document.createElementNS(SVG, "path");
      path.setAttribute("d", ICONS[b.k]);
      svg.appendChild(path);
      span.appendChild(svg);
    } else {
      return null;
    }
    return span;
  }

  function name(m) {
    var span = el("span", "name " + (m.role || ""));
    (m.b || []).forEach(function (b) { var x = badge(b); if (x) span.appendChild(x); });
    span.appendChild(document.createTextNode(m.a || ""));
    return span;
  }

  function runs(m, into) {
    (m.r || []).forEach(function (run) {
      if (run.i) into.appendChild(picture(run.i, "emoji", run.alt || ""));
      else if (run.x) into.appendChild(document.createTextNode(run.x));
      else if (run.alt) into.appendChild(document.createTextNode(run.alt));
    });
  }

  function paint(node, colors, head, body) {
    if (!colors) return;
    if (colors.hb) head.style.backgroundColor = colors.hb;
    if (colors.ht) head.style.color = colors.ht;
    if (colors.bb && body) body.style.backgroundColor = colors.bb;
    if (colors.bt && body) body.style.color = colors.bt;
  }

  function row(m) {
    var node;

    if (m.k === 1) {                                   // Super Chat
      node = el("div", "card paid");
      var head = el("div", "head");
      var who = el("div", "who");
      who.appendChild(name(m));
      who.appendChild(el("div", "amount", m.m || ""));
      head.append(avatar(m), who);
      node.appendChild(head);
      var body = null;
      if (m.r && m.r.length) { body = el("div", "text"); runs(m, body); node.appendChild(body); }
      paint(node, m.c, head, body);
      return node;
    }

    if (m.k === 2 || m.k === 3) {                      // new member, gift
      node = el("div", "card " + (m.k === 2 ? "member" : "gift"));
      var h = el("div", "head");
      var w = el("div", "who");
      w.appendChild(name(m));
      if (m.s) w.appendChild(el("div", "sub", m.s));
      h.append(avatar(m), w);
      node.appendChild(h);
      if (m.r && m.r.length) { var t = el("div", "text"); runs(m, t); node.appendChild(t); }
      return node;
    }

    node = el("div", "msg");
    var content = el("div", "body");
    content.appendChild(el("span", "t", stamp(m.t)));
    content.appendChild(name(m));
    runs(m, content);
    node.append(avatar(m), content);
    return node;
  }

  function nearBottom() { return list.scrollHeight - list.scrollTop - list.clientHeight < 60; }

  function follow(instant) {
    list.classList.toggle("jump", instant);
    list.scrollTop = list.scrollHeight;
    autoUntil = now() + 0.8;
    more.classList.remove("on");
  }

  function reveal() {
    var t = clockNow();
    var due = 0;
    while (due < pending.length && pending[due].t <= t) due++;
    if (!due) return;

    var batch = pending.splice(0, due);
    var instant = batch.length > ANIMATED;

    batch.forEach(function (m, i) {
      var node = row(m);
      if (i >= batch.length - ANIMATED) node.classList.add("enter");
      list.appendChild(node);
    });

    while (list.children.length > MAX_ROWS) list.removeChild(list.firstChild);

    if (stuck) follow(instant);
    else more.classList.add("on");
  }

  function clear() {
    list.textContent = "";
    pending = [];
    seen = Object.create(null);
    more.classList.remove("on");
    stuck = true;
  }

  function take(messages) {
    messages.forEach(function (m) {
      var id = m.id || (m.t + m.a);
      if (seen[id]) return;
      seen[id] = true;
      pending.push(m);
    });
    pending.sort(function (a, b) { return a.t - b.t; });
  }

  function render(data) {
    var before = clockNow();
    if (clock.known && Math.abs(data.time - before) > SEEK && !(data.time === clock.last)) clear();

    clock.playing = !data.ended && data.time !== clock.last;
    clock.pos = data.time;
    clock.at = now();
    clock.last = data.time;
    clock.known = true;

    document.title = "Chat replay - " + data.title;
    titleEl.textContent = data.title;
    clockEl.textContent = stamp(data.time);
    statusEl.textContent = data.status;

    take(data.messages);
    reveal();
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

  function frame() {
    if (clock.known) { reveal(); clockEl.textContent = stamp(clockNow()); }
    requestAnimationFrame(frame);
  }

  // The user scrolled up: stop following. Scrolling back down starts it again.
  list.addEventListener("wheel", function (e) { if (e.deltaY < 0) stuck = false; }, { passive: true });
  list.addEventListener("touchmove", function () { stuck = false; }, { passive: true });
  list.addEventListener("scroll", function () {
    if (nearBottom()) { stuck = true; more.classList.remove("on"); }
    else if (now() > autoUntil) stuck = false;
  });
  more.addEventListener("click", function () { stuck = true; follow(false); });

  poll();
  setInterval(poll, 700);
  requestAnimationFrame(frame);
})();
</script>
</body>
</html>
`
