// A minimal DOM double for behavioral module tests; browser layout remains an E2E responsibility.
export function createDocument() {
    const doc = { activeElement: null };
    class Element {
        constructor(tag) { this.tagName = tag.toUpperCase(); this.children = []; this.attributes = {}; this.listeners = new Map(); this.dataset = {}; this.hidden = false; this.value = ''; this.checked = false; this.textContent = ''; this.className = ''; this.disabled = false; }
        appendChild(child) { this.children.push(child); return child; }
        replaceChildren(...children) { this.children = children; }
        setAttribute(key, value) { this.attributes[key] = String(value); }
        getAttribute(key) { return this.attributes[key] ?? null; }
        addEventListener(type, listener) { if (!this.listeners.has(type)) this.listeners.set(type, new Set()); this.listeners.get(type).add(listener); }
        removeEventListener(type, listener) { this.listeners.get(type)?.delete(listener); }
        emit(type) { if (type === 'click' && this.onclick) this.onclick({ target: this }); for (const listener of this.listeners.get(type) || []) listener({ target: this }); }
        focus() { doc.activeElement = this; }
        get classList() { return { add: () => {}, remove: () => {}, toggle: () => {} }; }
        querySelector(selector) { return walk(this).find(element => selector.startsWith('#') ? element.id === selector.slice(1) : selector === '[data-metadata-status]' ? element.dataset.metadataStatus !== undefined : false) || null; }
    }
    doc.createElement = tag => new Element(tag);
    return doc;
}
export function walk(node) { return [node, ...node.children.flatMap(walk)]; }
