export function navigationFixture({ mobile = false, collapsed = false, storageBlocked = false } = {}) {
    const events = target => {
        target.listeners = new Map();
        target.addEventListener = (type, fn) => { if (!target.listeners.has(type)) target.listeners.set(type, new Set()); target.listeners.get(type).add(fn); };
        target.removeEventListener = (type, fn) => target.listeners.get(type)?.delete(fn);
        target.emit = (type, extra = {}) => {
            const event = { target, currentTarget: target, defaultPrevented: false, preventDefault() { this.defaultPrevented = true; }, stopPropagation() {}, ...extra };
            for (const fn of [...(target.listeners.get(type) || [])]) fn(event);
            return event;
        };
        return target;
    };
    const doc = events({ activeElement: null });
    const media = events({ matches: mobile });
    function node(tag, attrs = {}, parent = null) {
        const element = events({ tagName: tag.toUpperCase(), attributes: { ...attrs }, children: [], parentElement: parent, hidden: false, inert: false, disabled: false, isConnected: true, textContent: '' });
        element.getAttribute = name => element.attributes[name] ?? null;
        element.setAttribute = (name, value) => { element.attributes[name] = String(value); };
        element.removeAttribute = name => { delete element.attributes[name]; };
        Object.defineProperty(element, 'id', { get: () => element.attributes.id || '', set: value => { element.attributes.id = value; } });
        element.classes = new Set();
        element.classList = { add: name => element.classes.add(name), remove: name => element.classes.delete(name), contains: name => element.classes.has(name), toggle(name, on) { if (on ?? !element.classes.has(name)) element.classes.add(name); else element.classes.delete(name); } };
        element.contains = child => element === child || element.children.some(item => item.contains(child));
        element.closest = () => { let current = element; while (current) { if (current.hidden || current.inert) return current; current = current.parentElement; } return null; };
        element.getClientRects = () => (element.hidden || element === desktopToggle && media.matches || element === closeButton && !media.matches || element === mobileToggle && !media.matches) ? [] : [{}];
        element.focus = () => { doc.activeElement = element; doc.emit('focusin', { target: element }); };
        element.replaceChildren = () => { element.children = []; };
        if (parent) parent.children.push(element);
        return element;
    }
    const body = node('body'); doc.body = body;
    const skip = node('a', { href: '#content' }, body);
    const layout = node('div', {}, body);
    const sidebar = node('aside', { id: 'workspace-sidebar' }, layout);
    const desktopToggle = node('button', { 'data-sidebar-toggle': '' }, sidebar);
    const closeButton = node('button', { 'data-navigation-close': '' }, sidebar);
    const link = node('a', { href: '/' }, sidebar); link.textContent = '新建任务';
    const lastLink = node('a', { href: '/settings' }, sidebar); lastLink.textContent = '设置';
    const backdrop = node('button', { 'data-navigation-backdrop': '' }, layout); backdrop.hidden = true;
    const main = node('div', {}, layout);
    const mobileToggle = node('button', { 'data-navigation-toggle': '' }, main);
    const content = node('main', { id: 'content' }, main);
    const input = node('input', {}, content);
    const map = new Map([['.workspace-layout', layout], ['.workspace-sidebar', sidebar], ['[data-sidebar-toggle]', desktopToggle], ['[data-navigation-toggle]', mobileToggle], ['[data-navigation-close]', closeButton], ['[data-navigation-backdrop]', backdrop]]);
    doc.querySelector = selector => map.get(selector) || null;
    doc.querySelectorAll = selector => selector === '.main-nav .nav-link' ? [link, lastLink] : map.has(selector) ? [map.get(selector)] : [];
    sidebar.querySelectorAll = selector => selector === '.nav-link' || selector === '.main-nav .nav-link' ? [link, lastLink] : [desktopToggle, closeButton, link, lastLink];
    sidebar.querySelector = () => null;
    doc.getElementById = id => id === 'content' ? content : id === 'workspace-sidebar' ? sidebar : null;
    const stored = new Map(collapsed ? [['workspace.sidebar.collapsed', 'true']] : []);
    const writes = [];
    const win = {
        location: { pathname: '/', origin: 'https://local.test', replace() {} },
        matchMedia: query => { if (query !== '(max-width: 767px)') throw new Error('breakpoint mismatch'); return media; },
        localStorage: { getItem(key) { if (storageBlocked) throw new Error('blocked'); return stored.get(key) ?? null; }, setItem(key, value) { if (storageBlocked) throw new Error('blocked'); writes.push([key, value]); stored.set(key, value); } },
    };
    const resize = value => { media.matches = value; media.emit('change', { matches: value }); };
    return { win, doc, body, layout, sidebar, desktopToggle, closeButton, mobileToggle, backdrop, main, content, input, skip, link, lastLink, media, resize, stored, writes };
}
