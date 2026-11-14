import ReactDOM from 'react-dom'
import './index.css'
import config, { loadRuntimeConfig } from './config'
import { registerSW } from 'virtual:pwa-register'

window.global = window // Used by react-image-lightbox.
registerSW({ immediate: true })

const root = document.getElementById('root')
const callbackParams = new URLSearchParams(window.location.search)
let callbackToken = callbackParams.has('token')
  ? callbackParams.get('token')
  : undefined
if (callbackToken !== undefined) {
  callbackParams.delete('token')
  const query = callbackParams.toString()
  window.history.replaceState(
    null,
    '',
    `${window.location.pathname}${query ? `?${query}` : ''}${window.location.hash}`,
  )
}

let starting = false
async function start() {
  if (starting) return
  starting = true
  ReactDOM.render(
    <main className="app-startup">
      <p role="status">正在连接音乐库…</p>
    </main>,
    root,
  )
  try {
    await loadRuntimeConfig()
    if (callbackToken !== undefined) {
      const { default: authProvider } = await import('./authProvider')
      try {
        await authProvider.login({ token: callbackToken })
      } catch (error) {
        if (error.status === 401 && config.emosLoginURL) {
          window.location.replace(config.emosLoginURL)
          return
        }
        throw error
      }
      callbackToken = undefined
      window.history.replaceState(
        null,
        '',
        `${window.location.pathname}${window.location.search}#/`,
      )
    }
    const { default: App } = await import('./App')
    ReactDOM.render(<App />, root)
  } catch (error) {
    ReactDOM.render(
      <main className="app-startup" role="alert">
        <h1>暂时没连上音乐库</h1>
        <p>
          {error.message === 'errors.network_error'
            ? '暂时无法连接播放器，请稍后重试。'
            : error.message || '请稍后重试。'}
        </p>
        <button onClick={start}>重试</button>
        {config.emosLoginURL && (
          <a href={config.emosLoginURL}>使用 EMOS 重新登录</a>
        )}
      </main>,
      root,
    )
  } finally {
    starting = false
  }
}

start()
