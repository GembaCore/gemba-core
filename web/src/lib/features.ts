export const features = {
  mail: import.meta.env.VITE_MAIL_ENABLED === '1',
} as const;
