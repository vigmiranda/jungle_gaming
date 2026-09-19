// Progressive load — smoke de /health/live (não cobre o diferencial do enunciado).
// Carga formal: docs/load-testing.md e scripts/run-load-test.
// Exemplo:
//   k6 run -e BASE_URL=http://localhost:8090 loadtests/progressive-load.js
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
