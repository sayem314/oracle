// oracle is an authenticated, cookie-based app; render it entirely on the
// client so every API call carries the session cookie without SSR forwarding.
export const ssr = false;
