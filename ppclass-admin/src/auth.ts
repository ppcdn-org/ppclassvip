const TOKEN_KEY = 'ppclass.adminToken';

export function loadAdminToken() {
  return localStorage.getItem(TOKEN_KEY) || '';
}

export function saveAdminToken(token: string) {
  localStorage.setItem(TOKEN_KEY, token);
}

export function clearAdminToken() {
  localStorage.removeItem(TOKEN_KEY);
}
