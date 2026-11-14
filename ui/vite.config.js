import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import { VitePWA } from 'vite-plugin-pwa'

const frontendPort = parseInt(process.env.PORT) || 4533
const backendPort = frontendPort + 100
const basePath = '/navidrome'
const appBasePath = '/web/'

// https://vitejs.dev/config/
export default defineConfig({
  plugins: [
    react(),
    VitePWA({
      scope: appBasePath,
      includeAssets: [
        '*.ico',
        '*.png',
        '*.svg',
        'browserconfig.xml',
        'fonts/*.woff2',
      ],
      includeManifestIcons: false,
      manifest: manifest(),
      strategies: 'injectManifest',
      srcDir: 'src',
      filename: 'sw.js',
      injectManifest: {
        maximumFileSizeToCacheInBytes: 3 * 1024 * 1024, // 3 MiB
        modifyURLPrefix: { '': appBasePath },
        dontCacheBustURLsMatching: new RegExp(`^${appBasePath}assets/`),
      },
      integration: {
        beforeBuildServiceWorker(options) {
          // Vite adds icons and the manifest after Workbox applies modifyURLPrefix.
          options.injectManifest.additionalManifestEntries =
            options.injectManifest.additionalManifestEntries.map((entry) => ({
              ...entry,
              url: entry.url.startsWith(appBasePath)
                ? entry.url
                : `${appBasePath}${entry.url}`,
            }))
        },
      },
      devOptions: {
        enabled: true,
      },
    }),
  ],
  server: {
    host: true,
    port: frontendPort,
    proxy: {
      [`^${basePath}/(auth|api|rest|backgrounds|share|ui-config)(/.*)?$`]:
        'http://localhost:' + backendPort,
    },
  },
  base: appBasePath,
  define: {
    // JSONForms and other libraries use process.env
    'process.env': JSON.stringify({}),
  },
  build: {
    outDir: 'build',
    sourcemap: true,
  },
  test: {
    globals: true,
    environment: 'jsdom',
    setupFiles: './src/setupTests.js',
    css: true,
    reporters: ['verbose'],
    // reporters: ['default', 'hanging-process'],
    coverage: {
      reporter: ['text', 'json', 'html'],
      include: ['src/**/*'],
      exclude: [],
    },
  },
})

// PWA manifest
function manifest() {
  return {
    name: 'Navidrome',
    short_name: 'Navidrome',
    description:
      'Navidrome, an open source web-based music collection server and streamer',
    categories: ['music', 'entertainment'],
    display: 'standalone',
    id: appBasePath,
    start_url: appBasePath,
    scope: appBasePath,
    lang: 'zh-Hans',
    background_color: 'white',
    theme_color: 'blue',
    icons: [
      {
        src: `${appBasePath}android-chrome-192x192.png`,
        sizes: '192x192',
        type: 'image/png',
      },
      {
        src: `${appBasePath}android-chrome-512x512.png`,
        sizes: '512x512',
        type: 'image/png',
      },
    ],
  }
}
