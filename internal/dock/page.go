package dock

// pageHTML est le panneau affiché dans OBS.
//
// Les docks d'OBS sont étroits et le thème par défaut est sombre : une seule
// colonne, des couleurs alignées sur l'interface hôte, et l'information la plus
// utile — suis-je à jour, ai-je des modifications non publiées — lisible sans
// cliquer.
const pageHTML = `<!doctype html>
<html lang="fr">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Régie</title>
<style>
  :root {
    --bg: #1e1e1e; --panel: #272727; --line: #3b3b3b;
    --ink: #e8e8e8; --ink-2: #9aa0a6;
    --ok: #4ebe91; --warn: #e0a33a; --accent: #ff6275;
  }
  * { box-sizing: border-box; }
  body {
    margin: 0; padding: 12px;
    background: var(--bg); color: var(--ink);
    font: 13px/1.5 "Segoe UI", system-ui, sans-serif;
  }
  .row { display: flex; align-items: baseline; justify-content: space-between; gap: 8px; }
  .version { font-size: 15px; font-weight: 600; }
  .author { color: var(--ink-2); font-size: 12px; }
  .badge {
    display: inline-block; padding: 2px 7px; border-radius: 3px;
    font-size: 11px; font-weight: 600; letter-spacing: .03em;
  }
  .badge-ok { background: rgba(78,190,145,.16); color: var(--ok); }
  .badge-warn { background: rgba(224,163,58,.16); color: var(--warn); }
  .card {
    background: var(--panel); border: 1px solid var(--line);
    border-radius: 4px; padding: 10px 12px; margin-top: 10px;
  }
  h2 {
    margin: 0 0 6px; font-size: 11px; font-weight: 600;
    text-transform: uppercase; letter-spacing: .07em; color: var(--ink-2);
  }
  ul { margin: 0; padding-left: 18px; }
  li { margin-bottom: 3px; }
  label { display: flex; gap: 8px; align-items: flex-start; cursor: pointer; }
  input[type=checkbox] { margin-top: 3px; flex: none; }
  input[type=text] {
    width: 100%; margin-top: 8px; padding: 6px 8px;
    background: var(--bg); color: var(--ink);
    border: 1px solid var(--line); border-radius: 3px; font: inherit;
  }
  input[type=text]:focus, label:focus-within { outline: 2px solid var(--accent); outline-offset: 1px; }
  .hint { color: var(--ink-2); font-size: 12px; margin: 6px 0 0; }
  .err { color: var(--accent); font-size: 12px; margin-top: 8px; }
  [hidden] { display: none !important; }
</style>
</head>
<body>

<div class="row">
  <span class="version" id="version">—</span>
  <span class="badge badge-ok" id="status">chargement</span>
</div>
<div class="author" id="author"></div>

<div class="card" id="publishCard" hidden>
  <h2>Publication</h2>
  <label>
    <input type="checkbox" id="publish">
    <span>Publier mes modifications en quittant OBS</span>
  </label>
  <input type="text" id="message" placeholder="Ce que vous avez changé (facultatif)">
  <p class="hint" id="publishHint"></p>
</div>

<div class="card" id="todoCard" hidden>
  <h2>Sources à configurer</h2>
  <ul id="todo"></ul>
  <p class="hint">Réglez l'appareil une fois : il sera conservé à chaque mise à jour.</p>
</div>

<div class="card" id="readonlyCard" hidden>
  <h2>Lecture seule</h2>
  <p class="hint">Ce poste reçoit les scènes de l'équipe. Publier demande un fichier publisher.json.</p>
</div>

<p class="err" id="error" hidden></p>

<script>
  const $ = (id) => document.getElementById(id);
  let sending = false;

  async function refresh() {
    if (sending) return;
    try {
      const st = await (await fetch('/api/state', { cache: 'no-store' })).json();
      render(st);
    } catch (e) {
      $('status').textContent = 'agent arrêté';
      $('status').className = 'badge badge-warn';
    }
  }

  function render(st) {
    $('version').textContent = st.version ? 'Version ' + st.version : 'Régie';
    $('author').textContent = st.author ? 'publiée par ' + st.author : '';

    if (st.has_changes) {
      $('status').textContent = 'modifié';
      $('status').className = 'badge badge-warn';
    } else {
      $('status').textContent = 'à jour';
      $('status').className = 'badge badge-ok';
    }

    $('publishCard').hidden = !st.can_publish;
    $('readonlyCard').hidden = st.can_publish;
    if (st.can_publish) {
      if (document.activeElement !== $('publish')) $('publish').checked = st.will_publish;
      if (document.activeElement !== $('message')) $('message').value = st.message || '';
      $('publishHint').textContent = st.has_changes
        ? 'Vos scènes diffèrent de la version en ligne.'
        : 'Aucune modification détectée pour l instant.';
    }

    const todo = st.unconfigured || [];
    $('todoCard').hidden = todo.length === 0;
    $('todo').innerHTML = '';
    for (const name of todo) {
      const li = document.createElement('li');
      li.textContent = name;
      $('todo').appendChild(li);
    }

    $('error').hidden = !st.error;
    $('error').textContent = st.error || '';
  }

  async function sendIntent() {
    sending = true;
    try {
      await fetch('/api/publish-intent', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ publish: $('publish').checked, message: $('message').value })
      });
    } finally {
      sending = false;
    }
  }

  $('publish').addEventListener('change', sendIntent);
  $('message').addEventListener('input', () => {
    clearTimeout(window._t);
    window._t = setTimeout(sendIntent, 400);
  });

  refresh();
  setInterval(refresh, 3000);
</script>
</body>
</html>
`
