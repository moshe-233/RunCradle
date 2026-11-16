/* Reuse RunCradle's original workbench. DSH supplies only the right-sidebar slot. */
window.__ModuleLoader__.load({ id: 'dsh-runcradle', factory: require => {
  const { createElement: h, useState, useEffect } = require('react');
  const Icon = () => h('svg', { width: 20, height: 20, viewBox: '0 0 24 24', fill: 'none', stroke: 'currentColor', strokeWidth: 1.7, 'aria-hidden': true }, h('path', { d: 'M5 5h14v14H5zM9 5v14M9 10h10' }));
  function apply(ctx) {
    function Panel() {
      const [config, setConfig] = useState(null), [error, setError] = useState('');
      useEffect(() => {
        const controller = new AbortController();
        void (async () => {
          try {
            const response = await fetch('/api/runcradle-workbench', { credentials: 'same-origin', cache: 'no-store', signal: controller.signal });
            if (!response.ok) throw new Error(`工作台接入未就绪（HTTP ${response.status}）。请使用 demo 启动命令。`);
            const value = await response.json();
            const url = new URL(value.url);
            if (url.protocol !== 'http:' || url.hostname !== '127.0.0.1' || !url.port) throw new Error('工作台地址无效');
            const key = `runcradle-paired:${url.origin}`;
            try { if (sessionStorage.getItem(key)) url.hash = ''; } catch { /* browser storage is optional */ }
            if (!controller.signal.aborted) setConfig({ src: url.href, key });
          } catch (e) { if (!controller.signal.aborted) setError(e.message); }
        })();
        return () => controller.abort();
      }, []);
      if (error || !config) return h('div', { role: 'status', style: { padding: 16, fontSize: 13, color: 'var(--dsw-alias-label-secondary)' } }, error || '正在连接工作台…');
      return h('iframe', { title: 'RunCradle 工作台', src: config.src, referrerPolicy: 'no-referrer', style: { display: 'block', border: 0, width: '100%', height: '100%' }, onLoad: () => { try { sessionStorage.setItem(config.key, '1'); } catch { /* optional */ } } });
    }
    ctx.effect(() => ctx.sidebarRightTabs.register({ id: 'dsh-runcradle', kind: 'runcradle', title: () => 'RunCradle 工作台', guide: [{ id: 'workbench', order: 5, title: () => 'RunCradle 工作台', description: () => '打开工作流编辑器与运行视图', icon: Icon }] }));
    ctx.slots.inject('sidebar.right.pane.tab', () => ctx.slots.register({ name: 'sidebar.right.pane.tab', key: 'dsh-runcradle' }, Panel));
  }
  return { inject: ['slots', 'sidebarRightTabs'], apply };
} });
