export const isWritable = (ownerId) => {
  return localStorage.getItem('userId') === ownerId
}

export const isReadOnly = (ownerId) => {
  return !isWritable(ownerId)
}

export const isSmartPlaylist = (pls) => !!pls.rules

export const canChangeTracks = (pls) =>
  pls.canEditTracks && !isSmartPlaylist(pls) && !pls.sync
