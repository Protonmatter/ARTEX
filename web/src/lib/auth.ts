const TOKEN_KEY = "reconrivet_token";
const LEGACY_TOKEN_KEY = "artex_token";
const COOKIE_MAX_AGE = 7 * 24 * 60 * 60; // 7 days (seconds)

export interface CurrentUser {
  id: string;
  name: string;
  username: string;
  email: string;
  avatar: string;
  role: string;
}

export const auth = {
  getToken(): string | null {
    if (typeof window === "undefined") return null;
    const current = localStorage.getItem(TOKEN_KEY);
    if (current) return current;
    const legacy = localStorage.getItem(LEGACY_TOKEN_KEY);
    if (legacy) {
      try {
        this.setToken(legacy);
      } catch {
        // A readable legacy session remains usable when storage rejects migration writes.
      }
      return legacy;
    }
    // Mock demo: no real login; return a fake token so the route guard allows going straight into the main UI.
    return process.env.NEXT_PUBLIC_MOCK === "1" ? "mock-demo" : null;
  },

  setToken(token: string): void {
    localStorage.setItem(TOKEN_KEY, token);
    // Also write a cookie for the Next.js middleware to read server-side
    document.cookie = `${TOKEN_KEY}=${encodeURIComponent(token)}; path=/; max-age=${COOKIE_MAX_AGE}; SameSite=Lax`;
    localStorage.removeItem(LEGACY_TOKEN_KEY);
    document.cookie = `${LEGACY_TOKEN_KEY}=; path=/; max-age=0`;
  },

  clearToken(): void {
    localStorage.removeItem(TOKEN_KEY);
    document.cookie = `${TOKEN_KEY}=; path=/; max-age=0`;
    localStorage.removeItem(LEGACY_TOKEN_KEY);
    document.cookie = `${LEGACY_TOKEN_KEY}=; path=/; max-age=0`;
  },

  // Parse the current user from the JWT payload's sub field; display only, no signature verification.
  getCurrentUser(): CurrentUser | null {
    const token = this.getToken();
    if (!token) return null;
    try {
      const parts = token.split(".");
      if (parts.length !== 3) return null;
      // base64url → base64
      const payload = JSON.parse(atob(parts[1].replace(/-/g, "+").replace(/_/g, "/")));
      const username: string = payload.sub === "ARTEX" ? "ReconRivet" : (payload.sub ?? "ReconRivet");
      return { id: "1", name: username, username, email: "", avatar: "", role: "operator" };
    } catch {
      return null;
    }
  },
};
