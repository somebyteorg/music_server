import { jwtDecode } from 'jwt-decode'
import { baseUrl } from './utils'
import config, { shareInfo } from './config'

const downloadsEnabled = config.enableDownloads
config.enableDownloads =
  downloadsEnabled &&
  (!localStorage.getItem('is-authenticated') ||
    Boolean(shareInfo) ||
    localStorage.getItem('canDownload') === 'true')

function storeAuthenticationInfo(authInfo) {
  authInfo.token && localStorage.setItem('token', authInfo.token)
  localStorage.setItem('userId', authInfo.id)
  localStorage.setItem('name', authInfo.name)
  localStorage.setItem('username', authInfo.username)
  if (authInfo.avatar) localStorage.setItem('avatar', authInfo.avatar)
  else localStorage.removeItem('avatar')
  localStorage.setItem('role', authInfo.isAdmin ? 'admin' : 'regular')
  localStorage.setItem('canDownload', String(authInfo.canDownload === true))
  config.enableDownloads = downloadsEnabled && authInfo.canDownload === true
  localStorage.setItem('is-authenticated', 'true')
}

const authProvider = {
  login: ({ username, password, token }) => {
    if (token !== undefined) removeItems()
    const url = baseUrl(token !== undefined ? '/auth/emos' : '/auth/login')
    const request = new Request(url, {
      method: 'POST',
      body: JSON.stringify(
        token !== undefined ? { token } : { username, password },
      ),
      headers: new Headers({ 'Content-Type': 'application/json' }),
    })
    return fetch(request)
      .then(async (response) => {
        if (response.status < 200 || response.status >= 300) {
          const body = await response.json().catch(() => ({}))
          const error = new Error(
            body.error || body.message || response.statusText,
          )
          error.status = response.status
          throw error
        }
        return response.json()
      })
      .then((response) => {
        jwtDecode(response.token) // Validate token
        storeAuthenticationInfo(response)
        return response
      })
      .catch((error) => {
        if (
          error.message === 'Failed to fetch' ||
          error.stack === 'TypeError: Failed to fetch'
        ) {
          throw new Error('errors.network_error')
        }

        throw error
      })
  },

  logout: () => {
    removeItems()
    return Promise.resolve()
  },

  checkAuth: () =>
    localStorage.getItem('is-authenticated')
      ? Promise.resolve()
      : Promise.reject(),

  checkError: ({ status }) => {
    if (status === 401) {
      removeItems()
      return Promise.reject()
    }
    return Promise.resolve()
  },

  getPermissions: () => {
    const role = localStorage.getItem('role')
    return role ? Promise.resolve(role) : Promise.reject()
  },

  getIdentity: () => {
    return Promise.resolve({
      id: localStorage.getItem('username'),
      fullName: localStorage.getItem('name'),
      avatar: localStorage.getItem('avatar'),
    })
  },
}

const removeItems = () => {
  config.enableDownloads = downloadsEnabled
  localStorage.removeItem('token')
  localStorage.removeItem('userId')
  localStorage.removeItem('name')
  localStorage.removeItem('username')
  localStorage.removeItem('avatar')
  localStorage.removeItem('role')
  localStorage.removeItem('subsonic-salt')
  localStorage.removeItem('subsonic-token')
  localStorage.removeItem('is-authenticated')
  localStorage.removeItem('canDownload')
}

export default authProvider
