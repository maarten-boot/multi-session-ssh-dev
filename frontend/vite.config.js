import {defineConfig} from 'vite';

// Vite 7's default target. Vite 6 (kept for Node 18 on Ubuntu 24.04)
// defaults to es2020/safari14, and down-levelling xterm.js 6 that far
// breaks full-screen apps such as vi.
export default defineConfig({
    build: {
        target: ['chrome107', 'edge107', 'firefox104', 'safari16'],
    },
});
