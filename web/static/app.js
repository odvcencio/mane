(function () {
  'use strict';

  // --- State ---
  let ws = null;
  let nextId = 1;
  const pending = {};
  const openTabs = []; // [{path, dirty}]
  let activeTab = -1;
  let editor = null;
  let connected = false;

  // --- DOM refs ---
  const tabbar = document.getElementById('tabbar');
  const fileTree = document.getElementById('file-tree');
  const editorContainer = document.getElementById('editor-container');
  const statusFile = document.getElementById('status-file');
  const statusPos = document.getElementById('status-pos');
  const statusLang = document.getElementById('status-lang');
  const connDot = document.getElementById('conn-dot');

  // --- WebSocket ---
  function connect() {
    const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
    ws = new WebSocket(proto + '//' + location.host + '/ws');

    ws.onopen = function () {
      connected = true;
      connDot.classList.remove('disconnected');
      rpc('listFiles', {}).then(renderFileTree);
    };

    ws.onclose = function () {
      connected = false;
      connDot.classList.add('disconnected');
      setTimeout(connect, 2000);
    };

    ws.onmessage = function (e) {
      try {
        const msg = JSON.parse(e.data);
        if (msg.id !== undefined && pending[msg.id]) {
          const p = pending[msg.id];
          delete pending[msg.id];
          if (msg.error) {
            p.reject(new Error(msg.error.message));
          } else {
            p.resolve(msg.result);
          }
        }
      } catch (err) {
        console.error('ws message error:', err);
      }
    };
  }

  function rpc(method, params) {
    return new Promise(function (resolve, reject) {
      const id = nextId++;
      pending[id] = { resolve: resolve, reject: reject };
      ws.send(JSON.stringify({ id: id, method: method, params: params }));
    });
  }

  // --- File tree ---
  function renderFileTree(result) {
    if (!result || !result.files) return;
    fileTree.innerHTML = '';
    result.files.forEach(function (path) {
      const div = document.createElement('div');
      div.className = 'file-item';
      div.textContent = path;
      div.title = path;
      div.onclick = function () { openFile(path); };
      fileTree.appendChild(div);
    });
  }

  // --- Tabs ---
  function openFile(path) {
    // Check if already open
    for (let i = 0; i < openTabs.length; i++) {
      if (openTabs[i].path === path) {
        switchTab(i);
        return;
      }
    }
    rpc('openFile', { path: path }).then(function (result) {
      openTabs.push({ path: path, dirty: false });
      activeTab = openTabs.length - 1;
      renderTabs();
      setEditorContent(result.text, result.language);
      updateStatus(path, result.language);
      highlightActiveFile(path);
    }).catch(function (err) {
      console.error('open file error:', err);
    });
  }

  function switchTab(index) {
    if (index < 0 || index >= openTabs.length) return;
    activeTab = index;
    const tab = openTabs[index];
    rpc('readBuffer', { path: tab.path }).then(function (result) {
      setEditorContent(result.text);
      renderTabs();
      rpc('getLanguage', { path: tab.path }).then(function (lr) {
        if (editor && lr.language) {
          monaco.editor.setModelLanguage(editor.getModel(), mapLanguage(lr.language));
        }
        updateStatus(tab.path, lr.language);
      });
      highlightActiveFile(tab.path);
    });
  }

  function closeTab(index) {
    openTabs.splice(index, 1);
    if (openTabs.length === 0) {
      activeTab = -1;
      if (editor) editor.setValue('');
      statusFile.textContent = '';
      statusLang.textContent = '';
    } else {
      if (activeTab >= openTabs.length) activeTab = openTabs.length - 1;
      switchTab(activeTab);
    }
    renderTabs();
  }

  function renderTabs() {
    tabbar.innerHTML = '';
    openTabs.forEach(function (tab, i) {
      const el = document.createElement('div');
      el.className = 'tab' + (i === activeTab ? ' active' : '');

      const name = tab.path.split('/').pop();
      el.innerHTML = name +
        (tab.dirty ? '<span class="dirty">M</span>' : '') +
        '<span class="close">&times;</span>';

      el.onclick = function (e) {
        if (e.target.classList.contains('close')) {
          closeTab(i);
        } else {
          switchTab(i);
        }
      };
      tabbar.appendChild(el);
    });
  }

  // --- Editor ---
  function initEditor() {
    require.config({ paths: { vs: 'https://cdn.jsdelivr.net/npm/monaco-editor@0.45.0/min/vs' } });
    require(['vs/editor/editor.main'], function () {
      editor = monaco.editor.create(editorContainer, {
        value: '',
        language: 'plaintext',
        theme: 'vs-dark',
        automaticLayout: true,
        fontSize: 14,
        fontFamily: "'Consolas', 'Monaco', 'Courier New', monospace",
        minimap: { enabled: true },
        scrollBeyondLastLine: false,
        lineNumbers: 'on',
        renderWhitespace: 'selection',
        tabSize: 4,
        wordWrap: 'off',
      });

      editor.onDidChangeCursorPosition(function (e) {
        statusPos.textContent = 'Ln ' + e.position.lineNumber + ', Col ' + e.position.column;
      });

      editor.onDidChangeModelContent(function () {
        if (activeTab >= 0 && activeTab < openTabs.length) {
          openTabs[activeTab].dirty = true;
          renderTabs();
          // Debounced sync to server
          clearTimeout(openTabs[activeTab]._syncTimer);
          openTabs[activeTab]._syncTimer = setTimeout(function () {
            rpc('writeBuffer', {
              path: openTabs[activeTab].path,
              text: editor.getValue()
            });
          }, 500);
        }
      });

      // Ctrl+S to save
      editor.addCommand(monaco.KeyMod.CtrlCmd | monaco.KeyCode.KeyS, function () {
        if (activeTab >= 0) {
          rpc('writeBuffer', {
            path: openTabs[activeTab].path,
            text: editor.getValue()
          }).then(function () {
            return rpc('saveFile', { path: openTabs[activeTab].path });
          }).then(function () {
            openTabs[activeTab].dirty = false;
            renderTabs();
          });
        }
      });

      connect();
    });
  }

  function setEditorContent(text, language) {
    if (!editor) return;
    editor.setValue(text || '');
    if (language) {
      monaco.editor.setModelLanguage(editor.getModel(), mapLanguage(language));
    }
  }

  function mapLanguage(lang) {
    const map = {
      go: 'go',
      typescript: 'typescript',
      javascript: 'javascript',
      python: 'python',
      rust: 'rust',
      c: 'c',
      cpp: 'cpp',
      java: 'java',
      lua: 'lua',
      json: 'json',
      html: 'html',
      css: 'css',
      markdown: 'markdown',
      yaml: 'yaml',
      toml: 'plaintext',
      bash: 'shell',
      sh: 'shell',
    };
    return map[lang] || 'plaintext';
  }

  // --- Status bar ---
  function updateStatus(path, language) {
    statusFile.textContent = path || '';
    statusLang.textContent = (language || 'plaintext').toUpperCase();
  }

  function highlightActiveFile(path) {
    var items = fileTree.querySelectorAll('.file-item');
    items.forEach(function (item) {
      item.classList.toggle('active', item.textContent === path);
    });
  }

  // --- Init ---
  initEditor();
})();
