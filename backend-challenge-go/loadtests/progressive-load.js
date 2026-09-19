# Progressive load (opcional — STRESS_TESTS §13).
# Requer k6 instalado e ambiente stress no ar.
# Exemplo:
#   export BASE_URL=http://localhost:8090 PROVIDER_TOKEN=... 
#   k6 run loadtests/progressive-load.js
import http from "k6/http";
import { sleep } from "k6";

export const options = {
  stages: [
    { duration: "30s", target: 10 },
    { duration: "30s", target: 50 },
    { duration: "30s", target: 10 },
    { duration: "10s", target: 0 },
  ],
};

const base = __ENV.BASE_URL || "http://localhost:8090";

export default function () {
  http.get(`${base}/health/live`);
  sleep(0.1);
}
