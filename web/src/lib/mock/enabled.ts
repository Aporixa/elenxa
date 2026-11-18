// Demo/mock mode (a backend-less public demo build) has been removed from this
// fork. MOCK is a constant false so the remaining `if (MOCK)` branches are dead
// code; the env var NEXT_PUBLIC_MOCK no longer has any effect.
export const MOCK = false;
