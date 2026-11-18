import * as React from 'react'
import { Lock } from '@material-ui/icons'
import { BulkUpdateButton, useTranslate } from 'react-admin'

const ChangePublicStatusButton = (props) => {
  const translate = useTranslate()
  const playlists = { public: false }
  const label = translate('resources.playlist.actions.makePrivate')
  return (
    <BulkUpdateButton
      {...props}
      data={playlists}
      label={label}
      icon={<Lock />}
    />
  )
}

export default ChangePublicStatusButton
