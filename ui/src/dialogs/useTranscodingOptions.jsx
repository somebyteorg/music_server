// Downloads and shares always use the original resource.
export const useTranscodingOptions = () => ({
  TranscodingOptionsInput: () => null,
  format: 'raw',
  maxBitRate: 0,
  originalFormat: true,
})
