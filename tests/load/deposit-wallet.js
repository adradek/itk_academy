import http from 'k6/http';
import exec from 'k6/execution';
import { check } from 'k6';
import { Counter } from 'k6/metrics';

const baseURL = (__ENV.BASE_URL || 'http://localhost:8000').replace(/\/$/, '');
const rate = 1000;
const durationSeconds = 50;
const amount = 1;
const requestTimeout = '5s';
const gracefulStopSeconds = 10;

const successfulDeposits = new Counter('successful_deposits');
const rejectedDeposits = new Counter('rejected_deposits');
const unknownOutcomes = new Counter('unknown_outcomes');

export const options = {
  scenarios: {
    deposit_wallet: {
      executor: 'constant-arrival-rate',
      rate,
      timeUnit: '1s',
      duration: `${durationSeconds}s`,
      preAllocatedVUs: 500,
      maxVUs: 500,
      gracefulStop: `${gracefulStopSeconds}s`,
    },
    verify_balance: {
      executor: 'shared-iterations',
      exec: 'verifyBalance',
      vus: 1,
      iterations: 1,
      startTime: `${durationSeconds + gracefulStopSeconds}s`,
      maxDuration: '10s',
      gracefulStop: '0s',
    },
  },
  thresholds: {
    checks: ['rate==1'],
    http_req_failed: ['rate==0'],
    dropped_iterations: ['count==0'],
    successful_deposits: [`count>=${rate * durationSeconds}`],
    rejected_deposits: ['count==0'],
    unknown_outcomes: ['count==0'],
    'http_reqs{scenario:deposit_wallet}': [`count>=${rate * durationSeconds}`],
    'http_req_duration{scenario:deposit_wallet}': ['p(95)<200', 'p(99)<500'],
  },
  summaryTrendStats: ['min', 'med', 'p(95)', 'p(99)', 'max'],
};

export function setup() {
  successfulDeposits.add(0);
  rejectedDeposits.add(0);
  unknownOutcomes.add(0);

  const response = http.post(`${baseURL}/api/v1/wallets`, null, {
    timeout: requestTimeout,
    redirects: 0,
    tags: { name: 'create_wallet' },
  });
  const wallet = responseJSON(response);
  if (response.status !== 201 || typeof wallet?.walletId !== 'string' || wallet.balance !== 0) {
    throw new Error(`Не удалось создать пустой кошелёк: HTTP ${response.status}`);
  }

  console.log(`walletId=${wallet.walletId}, исходный баланс=0, сумма операции=${amount} цент`);
  return {
    walletId: wallet.walletId,
    payload: JSON.stringify({ walletId: wallet.walletId, operationType: 'DEPOSIT', amount }),
  };
}

export default function (data) {
  const response = http.post(`${baseURL}/api/v1/wallet`, data.payload, {
    headers: { 'Content-Type': 'application/json' },
    timeout: requestTimeout,
    redirects: 0,
    tags: { name: 'POST /api/v1/wallet' },
  });

  if (response.status === 200) {
    successfulDeposits.add(1);
  } else if (response.status >= 400 && response.status < 500) {
    rejectedDeposits.add(1);
  } else {
    unknownOutcomes.add(1);
  }

  const wallet = responseJSON(response);
  check(response, {
    'пополнение: HTTP 200': (r) => r.status === 200,
    'пополнение: верный walletId': () => wallet?.walletId === data.walletId,
    'пополнение: положительный целочисленный баланс': () =>
      Number.isSafeInteger(wallet?.balance) && wallet.balance >= amount,
  });
}

export function verifyBalance(data) {
  const completed = exec.instance.iterationsCompleted;
  const interrupted = exec.instance.iterationsInterrupted;
  const expectedBalance = completed * amount;

  const response = http.get(`${baseURL}/api/v1/wallets/${data.walletId}`, {
    timeout: requestTimeout,
    redirects: 0,
    tags: { name: 'GET /api/v1/wallets/{WALLET_UUID}' },
  });
  const wallet = responseJSON(response);
  check(response, {
    'итог: нет прерванных итераций': () => interrupted === 0,
    'итог: HTTP 200': (r) => r.status === 200,
    'итог: верный walletId': () => wallet?.walletId === data.walletId,
    'итог: баланс равен сумме завершённых операций': () =>
      Number.isSafeInteger(expectedBalance) && wallet?.balance === expectedBalance,
  });

  console.log(`completed=${completed}, interrupted=${interrupted}, ` +
    `ожидаемый баланс при успехе всех POST=${expectedBalance}, фактический=${wallet?.balance}`);
}

function responseJSON(response) {
  try {
    return response.json();
  } catch {
    return null;
  }
}
