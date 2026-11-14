// Public UI settings are loaded before the React application is imported.
const defaultConfig = {
  version: 'dev',
  baseURL: '/navidrome',
  variousArtistsId: '63sqASlAfjbGMuLP4JhnZU', // See consts.VariousArtistsID in consts.go
  // Login backgrounds from https://unsplash.com/collections/1065384/music-wallpapers
  loginBackgroundURL: 'https://source.unsplash.com/collection/1065384/1600x900',
  maxSidebarPlaylists: 100,
  enableTranscodingConfig: true,
  enableDownloads: true,
  enableFavourites: true,
  losslessFormats: 'FLAC,WAV,ALAC,DSF',
  welcomeMessage: '',
  gaTrackingId: '',
  devActivityPanel: true,
  enableStarRating: true,
  defaultTheme: 'Dark',
  defaultLanguage: 'zh-Hans',
  emosLoginURL: '',
  defaultUIVolume: 100,
  uiSearchDebounceMs: 200,
  uiCoverArtSize: 600,
  enableUserEditing: true,
  enableArtworkUpload: true,
  enableSharing: true,
  shareURL: '',
  defaultDownloadableShare: true,
  devSidebarPlaylists: true,
  lastFMEnabled: true,
  listenBrainzEnabled: true,
  enableExternalServices: true,
  enableCoverAnimation: true,
  pidAlbum: 'musicbrainz_albumid|albumartistid,album,albumversion,releasedate', // See consts.DefaultAlbumPID
  pidTrack: 'musicbrainz_trackid|albumid,discnumber,tracknumber,title', // See consts.DefaultTrackPID
  enableNowPlaying: true,
  playbackReportIntervalMs: 60000,
  devShowArtistPage: true,
  devNewEventStream: false,
  enableReplayGain: true,
  enableQuickConnect: false,
  defaultDownsamplingFormat: 'opus',
  publicBaseUrl: '/share',
  separator: '/',
  enableInspect: true,
  pluginsEnabled: true,
}

const config = { ...defaultConfig }
export let shareInfo = null

async function getJSON(path, message) {
  let response
  try {
    response = await fetch(path, {
      headers: { Accept: 'application/json' },
      credentials: 'same-origin',
      cache: 'no-store',
      signal: AbortSignal.timeout(30_000),
    })
  } catch {
    throw new Error(message)
  }
  if (!response.ok) {
    if (response.status === 410) throw new Error('这个分享已过期。')
    if (response.status === 404) throw new Error('没有找到这个页面或分享。')
    throw new Error(message)
  }
  try {
    return await response.json()
  } catch {
    throw new Error(message)
  }
}

export async function loadRuntimeConfig() {
  const settings = await getJSON(
    '/navidrome/ui-config',
    '暂时无法连接播放器，请稍后重试。',
  )
  Object.assign(config, defaultConfig, settings)
  shareInfo = null
  const shareId = new URLSearchParams(window.location.search).get('share')
  if (!shareId) return
  if (!config.enableSharing) throw new Error('分享功能暂未开启。')
  shareInfo = await getJSON(
    `${config.baseURL}/share/${encodeURIComponent(shareId)}`,
    '暂时无法读取分享，请稍后重试。',
  )
  if (!shareInfo.id || !Array.isArray(shareInfo.tracks)) {
    throw new Error('暂时无法读取分享，请稍后重试。')
  }
  document.title = shareInfo.description || 'Navidrome'
  for (const [property, content] of Object.entries({
    'og:title': document.title,
    'og:url': shareInfo.url,
    'og:image': shareInfo.imageUrl,
  })) {
    if (!content) continue
    let meta = document.querySelector(`meta[property="${property}"]`)
    if (!meta) {
      meta = document.createElement('meta')
      meta.setAttribute('property', property)
      document.head.appendChild(meta)
    }
    meta.setAttribute('content', content)
  }
}

export default config
