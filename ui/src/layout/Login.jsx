import React, { useState, useCallback } from 'react'
import PropTypes from 'prop-types'
import { Field, Form } from 'react-final-form'
import { useDispatch } from 'react-redux'
import Button from '@material-ui/core/Button'
import Card from '@material-ui/core/Card'
import CardActions from '@material-ui/core/CardActions'
import CircularProgress from '@material-ui/core/CircularProgress'
import TextField from '@material-ui/core/TextField'
import { ThemeProvider, makeStyles } from '@material-ui/core/styles'
import {
  createMuiTheme,
  useLogin,
  useNotify,
  useTranslate,
  useVersion,
} from 'react-admin'
import Logo from '../icons/android-icon-192x192.png'

import Notification from './Notification'
import useCurrentTheme from '../themes/useCurrentTheme'
import config from '../config'
import { clearQueue } from '../actions'

const useStyles = makeStyles(
  (theme) => ({
    main: {
      display: 'flex',
      flexDirection: 'column',
      minHeight: '100vh',
      alignItems: 'center',
      justifyContent: 'flex-start',
      background: `url(${config.loginBackgroundURL})`,
      backgroundRepeat: 'no-repeat',
      backgroundSize: 'cover',
      backgroundPosition: 'center',
    },
    card: {
      minWidth: 300,
      marginTop: '6em',
      overflow: 'visible',
    },
    avatar: {
      margin: '1em',
      display: 'flex',
      justifyContent: 'center',
      marginTop: '-3em',
    },
    icon: {
      backgroundColor: 'transparent',
      width: '6.3em',
      height: '6.3em',
    },
    systemName: {
      marginTop: '1em',
      display: 'flex',
      justifyContent: 'center',
      color: '#3f51b5', //theme.palette.grey[500]
    },
    welcome: {
      marginTop: '1em',
      padding: '0 1em 1em 1em',
      display: 'flex',
      justifyContent: 'center',
      flexWrap: 'wrap',
      color: '#3f51b5', //theme.palette.grey[500]
    },
    form: {
      padding: '0 1em 1em 1em',
    },
    input: {
      marginTop: '1em',
    },
    actions: {
      flexDirection: 'column',
      alignItems: 'stretch',
      gap: theme.spacing(1.5),
      padding: theme.spacing(0, 2, 2.5),
    },
    actionButton: {
      minHeight: 44,
      fontSize: 14,
      textTransform: 'none',
    },
    loginDivider: {
      display: 'flex',
      alignItems: 'center',
      gap: theme.spacing(1.5),
      color: theme.palette.text.secondary,
      fontSize: 12,
      '&::before, &::after': {
        content: '""',
        flex: 1,
        borderTop: `1px solid ${theme.palette.divider}`,
      },
    },
    emosButton: {
      color: theme.palette.text.primary,
      borderColor: theme.palette.divider,
      boxShadow: 'none',
      '&:hover': {
        borderColor: theme.palette.text.secondary,
        backgroundColor: theme.palette.action.hover,
        boxShadow: 'none',
      },
    },
    emosIcon: {
      display: 'block',
      width: 20,
      height: 20,
      borderRadius: 4,
      objectFit: 'contain',
    },
    button: {},
    systemNameLink: {
      textDecoration: 'none',
    },
    message: {
      marginTop: '1em',
      padding: '0 1em 1em 1em',
      textAlign: 'center',
      wordBreak: 'break-word',
      fontSize: '0.875em',
    },
  }),
  { name: 'NDLogin' },
)

const renderInput = ({
  meta: { touched, error } = {},
  input: { ...inputProps },
  ...props
}) => (
  <TextField
    error={!!(touched && error)}
    inputProps={{
      // mobile keyboards: suppress capitalization and correction for login related fields
      autocapitalize: 'none',
      autocorrect: 'off',
      ...inputProps,
    }}
    helperText={touched && error}
    {...props}
    fullWidth
  />
)

const FormLogin = ({ loading, handleSubmit, validate }) => {
  const translate = useTranslate()
  const classes = useStyles()

  return (
    <Form
      onSubmit={handleSubmit}
      validate={validate}
      render={({ handleSubmit }) => (
        <form onSubmit={handleSubmit} noValidate>
          <div className={classes.main}>
            <Card className={classes.card}>
              <div className={classes.avatar}>
                <img src={Logo} className={classes.icon} alt={'logo'} />
              </div>
              <div className={classes.systemName}>
                <a
                  href="https://www.navidrome.org"
                  target="_blank"
                  rel="noopener noreferrer"
                  className={classes.systemNameLink}
                >
                  Navidrome
                </a>
              </div>
              {config.welcomeMessage && (
                <div
                  className={classes.welcome}
                  // Use dangerouslySetInnerHTML to allow admins to configure
                  // whatever content they want
                  dangerouslySetInnerHTML={{ __html: config.welcomeMessage }}
                />
              )}
              <div className={classes.form}>
                <div className={classes.input}>
                  <Field
                    autoFocus
                    name="username"
                    component={renderInput}
                    label={translate('ra.auth.username')}
                    disabled={loading}
                    spellCheck={false}
                  />
                </div>
                <div className={classes.input}>
                  <Field
                    name="password"
                    component={renderInput}
                    label={translate('ra.auth.password')}
                    type="password"
                    disabled={loading}
                  />
                </div>
              </div>
              <CardActions className={classes.actions} disableSpacing>
                <Button
                  variant="contained"
                  type="submit"
                  color="primary"
                  disabled={loading}
                  className={`${classes.button} ${classes.actionButton}`}
                  fullWidth
                >
                  {loading && <CircularProgress size={25} thickness={2} />}
                  {translate('ra.auth.sign_in')}
                </Button>
                {config.emosLoginURL && (
                  <>
                    <div className={classes.loginDivider} aria-hidden="true">
                      或
                    </div>
                    <Button
                      variant="outlined"
                      href={config.emosLoginURL}
                      disabled={loading}
                      className={`${classes.actionButton} ${classes.emosButton}`}
                      startIcon={
                        <img
                          src="https://files.emos.best/files/base/logo/logo_1.jpg"
                          alt=""
                          className={classes.emosIcon}
                          width="20"
                          height="20"
                        />
                      }
                      fullWidth
                    >
                      使用 EMOS 登录
                    </Button>
                  </>
                )}
              </CardActions>
            </Card>
            <Notification />
          </div>
        </form>
      )}
    />
  )
}

const Login = ({ location }) => {
  const [loading, setLoading] = useState(false)
  const translate = useTranslate()
  const notify = useNotify()
  const login = useLogin()
  const dispatch = useDispatch()

  const handleSubmit = useCallback(
    (auth) => {
      setLoading(true)
      dispatch(clearQueue())
      return login(
        auth,
        location?.state ? location.state.nextPathname : '/',
      ).catch((error) => {
        setLoading(false)
        notify(
          typeof error === 'string'
            ? error
            : typeof error === 'undefined' || !error.message
              ? 'ra.auth.sign_in_error'
              : error.message,
          'warning',
        )
      })
    },
    [dispatch, login, notify, setLoading, location],
  )

  const validateLogin = useCallback(
    (values) => {
      const errors = {}
      if (!values.username?.trim()) {
        errors.username = translate('ra.validation.required')
      }
      if (!values.password?.trim()) {
        errors.password = translate('ra.validation.required')
      }
      return errors
    },
    [translate],
  )

  return (
    <FormLogin
      handleSubmit={handleSubmit}
      validate={validateLogin}
      loading={loading}
    />
  )
}

Login.propTypes = {
  authProvider: PropTypes.func,
  previousRoute: PropTypes.string,
}

// We need to put the ThemeProvider decoration in another component
// Because otherwise the useStyles() hook used in Login won't get
// the right theme
const LoginWithTheme = (props) => {
  const theme = useCurrentTheme()
  const version = useVersion()

  return (
    <ThemeProvider theme={createMuiTheme(theme)}>
      <Login key={version} {...props} />
    </ThemeProvider>
  )
}

export default LoginWithTheme
