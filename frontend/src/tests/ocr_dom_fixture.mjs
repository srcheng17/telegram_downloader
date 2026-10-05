import { createDocument, walk } from './metadata_dom_fixture.mjs';
export { walk };
export function ocrDocument() {
    const doc = createDocument(); const create = doc.createElement; const events = new Map();
    doc.addEventListener = (type, fn) => { if (!events.has(type)) events.set(type, new Set()); events.get(type).add(fn); };
    doc.removeEventListener = (type, fn) => events.get(type)?.delete(fn);
    doc.dispatchEvent = event => events.get(event.type)?.forEach(fn => fn(event));
    doc.createElement = tag => {
        const node = create(tag); node.ownerDocument = doc;
        node.remove = () => { if (node.parentNode) node.parentNode.children = node.parentNode.children.filter(child => child !== node); node.parentNode = null; };
        node.appendChild = child => { child.remove?.(); child.parentNode = node; node.children.push(child); return child; };
        return node;
    };
    return doc;
}
export const control = (root, text) => walk(root).find(node => node.tagName === 'LABEL' && node.textContent === text)?.children[0];
export const click = (root, text) => { const button = walk(root).find(node => node.tagName === 'BUTTON' && node.textContent === text); if (!button) throw new Error('missing button ' + text); button.emit('click'); };
export const tick = () => new Promise(resolve => setImmediate(resolve));
