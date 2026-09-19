// duplicate-bet.js — ST-01 via k6 (opcional).
// Uso: k6 run -e BASE_URL=http://localhost:8090 -e TOKEN=... loadtests/duplicate-bet.js
import http from "k6/http";
import { check, fail } from "k6";

export const options = {
  vus: 50,
  iterations: 50,
  thresholds: {
    checks: ["rate==1"],
    http_req_failed: ["rate==0"],
  },
};

const base = __ENV.BASE_URL || "http://localhost:8090";
const token = __ENV.PROVIDER_TOKEN || "";
const walletId = __ENV.WALLET_ID || "";
const playerId = __ENV.PLAYER_ID || "";
const externalId = __ENV.EXTERNAL_ID || "k6-dup-bet";
const idemKey = `provider-a:${externalId}`;

export default function () {
  if (!token || !walletId || !playerId) {
    fail("defina PROVIDER_TOKEN, WALLET_ID e PLAYER_ID (pré-condições no setup manual)");
  }
  const res = http.post(
    `${base}/wagering/transactions`,
    JSON.stringify({
      providerId: "provider-a",
      externalTransactionId: externalId,
      playerId,
      walletId,
      roundId: "k6-round",
      gameId: "k6-game",
      kind: "BET",
      money: { amount: "10.00", currency: "BRL" },
    }),
    {
      headers: {
        Authorization: `Bearer ${token}`,
        "Content-Type": "application/json",
        "Idempotency-Key": idemKey,
        "X-Correlation-Id": `k6-${__VU}-${__ITER}`,
      },
    }
  );
  check(res, {
    "status 200": (r) => r.status === 200,
    "processed": (r) => {
      try {
        return JSON.parse(r.body).status === "PROCESSED";
      } catch (_) {
        return false;
      }
    },
  });
}
