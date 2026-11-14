import React from 'react'
import { BooleanInput, TextInput, required } from 'react-admin'
import config from '../config'

// Returned as an array, not a component, so SimpleForm still injects its props into each input.
export const playerInputs = () =>
  [
    <TextInput key="name" source="name" validate={[required()]} />,
    <BooleanInput key="reportRealPath" source="reportRealPath" fullWidth />,
    (config.lastFMEnabled || config.listenBrainzEnabled) && (
      <BooleanInput key="scrobbleEnabled" source="scrobbleEnabled" fullWidth />
    ),
  ].filter(Boolean)
