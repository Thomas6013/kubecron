package api

import "fmt"

// logSearchBar returns the toolbar HTML (search input + download link) that sits
// above the log terminal. runID is used to build the download link href.
func logSearchBar(runID string) string {
	return fmt.Sprintf(`<div style="display:flex;align-items:center;gap:10px;margin-bottom:8px;">
  <input id="log-search" type="text" placeholder="Filter by regex…"
    style="flex:1;background:var(--surface2);border:1px solid var(--border);border-radius:6px;
           padding:5px 10px;color:var(--text);font-family:var(--font-mono);font-size:12px;outline:none;">
  <span id="log-count" style="font-family:var(--font-mono);font-size:11px;color:var(--muted);white-space:nowrap;"></span>
  <a id="log-dl" href="/api/runs/%s/logs.txt" download
    style="font-family:var(--font-mono);font-size:11px;color:var(--accent);white-space:nowrap;">&#11015; .log</a>
</div>`, esc(runID))
}

// logSearchJS is the inline script that powers log search, level colorization,
// and the MutationObserver that handles live-streamed lines.
const logSearchJS = `<script>
(function(){
  var LEVELS = [
    {re:/FATAL|CRITICAL/i, cls:'log-fatal'},
    {re:/\bERROR\b|\bERR\b/i, cls:'log-error'},
    {re:/WARN(?:ING)?/i, cls:'log-warn'},
    {re:/\bINFO\b/i, cls:'log-info'},
    {re:/\bDEBUG\b/i, cls:'log-debug'}
  ];
  function levelClass(raw) {
    for (var i=0; i<LEVELS.length; i++) {
      if (LEVELS[i].re.test(raw)) return LEVELS[i].cls;
    }
    return '';
  }
  function renderLine(el, re) {
    var raw = el.dataset.raw || el.textContent;
    var lvl = levelClass(raw);
    if (re) {
      try {
        if (!re.test(raw)) { el.style.display='none'; return; }
        el.style.display='';
        el.className = 'll' + (lvl ? ' '+lvl : '');
        el.innerHTML = raw.replace(re, function(m){ return '<mark class="log-hl">'+m+'</mark>'; });
      } catch(e) { el.style.display=''; el.className='ll'+(lvl?' '+lvl:''); el.textContent=raw; }
    } else {
      el.style.display='';
      el.className = 'll' + (lvl ? ' '+lvl : '');
      el.textContent = raw;
    }
  }
  function applyFilter() {
    var pat = document.getElementById('log-search') ? document.getElementById('log-search').value : '';
    var re = null;
    if (pat) { try { re = new RegExp(pat,'gi'); } catch(e){} }
    var els = document.querySelectorAll('#log-term .ll');
    var shown=0;
    els.forEach(function(el){ renderLine(el,re); if(el.style.display!=='none') shown++; });
    var cnt = document.getElementById('log-count');
    if (cnt) cnt.textContent = pat ? shown+' / '+els.length+' lines' : els.length+' lines';
  }
  var srch = document.getElementById('log-search');
  if (srch) srch.addEventListener('input', applyFilter);
  document.querySelectorAll('#log-term .ll').forEach(function(el){ renderLine(el,null); });
  applyFilter();
  var term = document.getElementById('log-term');
  if (term) {
    var obs = new MutationObserver(function(mutations){
      var pat = srch ? srch.value : '';
      var re = null;
      if (pat) { try { re = new RegExp(pat,'gi'); } catch(e){} }
      mutations.forEach(function(m){
        m.addedNodes.forEach(function(node){
          if (node.classList && node.classList.contains('ll')) {
            renderLine(node, re);
          }
        });
      });
      if (!pat) term.scrollTop = term.scrollHeight;
      var els = document.querySelectorAll('#log-term .ll');
      var shown=0;
      els.forEach(function(el){ if(el.style.display!=='none') shown++; });
      var cnt = document.getElementById('log-count');
      if (cnt) cnt.textContent = pat ? shown+' / '+els.length+' lines' : els.length+' lines';
    });
    obs.observe(term, {childList:true});
  }
})();
</script>`
