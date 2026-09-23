import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
export default defineConfig({
    base: './',
    plugins: [react()],
    build: {
        outDir: '../internal/httpserver/static/dist',
        emptyOutDir: true,
        sourcemap: false,
    },
    server: {
        proxy: {
            '/api': 'http://localhost:8080',
        },
    },
});
