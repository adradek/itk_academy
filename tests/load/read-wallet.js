import http from 'k6/http';
import { check } from 'k6';

const baseURL = (__ENV.BASE_URL || 'http://localhost:8000').replace(/\/$/, '');
const rate = positiveInteger('RATE', 1000);
const durationSeconds = positiveInteger('DURATION_SECONDS', 60);
const initialBalance = 1000;
const requestTimeout = '5s';

export const options = {
  scenarios: {
    read_wallet: {
      executor: 'constant-arrival-rate',
      rate,
      timeUnit: '1s',
      duration: `${durationSeconds}s`,
      preAllocatedVUs: 500,
      maxVUs: 500,
      gracefulStop: '10s',
    },
  },
  thresholds: {
    checks: ['rate==1'],
    http_req_failed: ['rate==0'],
    dropped_iterations: ['count==0'],
    'http_reqs{scenario:read_wallet}': [`count>=${rate * durationSeconds}`],

    'http_req_duration{scenario:read_wallet}': ['p(95)<200', 'p(99)<500'],
  },
  summaryTrendStats: ['min', 'med', 'p(95)', 'p(99)', 'max'],
};

export function setup() {
  const created = http.post(`${baseURL}/api/v1/wallets`, null, {
    timeout: requestTimeout,
    tags: { name: 'create_wallet' },
  });
  const wallet = responseJSON(created);
  if (created.status !== 201 || typeof wallet?.walletId !== 'string' || wallet.balance !== 0) {
    throw new Error(`Не удалось создать пустой кошелёк: HTTP ${created.status}`);
  }

  const funded = http.post(`${baseURL}/api/v1/wallet`, JSON.stringify({
    walletId: wallet.walletId,
    operationType: 'DEPOSIT',
    amount: initialBalance,
  }), {
    headers: { 'Content-Type': 'application/json' },
    timeout: requestTimeout,
    tags: { name: 'fund_wallet' },
  });
  if (!checkWallet(funded, wallet.walletId, 'пополнение')) {
    throw new Error(`Не удалось пополнить кошелёк: HTTP ${funded.status}`);
  }

  console.log(`walletId=${wallet.walletId}, исходный баланс=${initialBalance} центов`);
  return { walletId: wallet.walletId };
}

export default function (data) {
  const response = readWallet(data.walletId);
  checkWallet(response, data.walletId, 'чтение');
}

export function teardown(data) {
  checkWallet(readWallet(data.walletId), data.walletId, 'итоговый баланс');
}

function readWallet(walletId) {
  return http.get(`${baseURL}/api/v1/wallets/${walletId}`, {
    timeout: requestTimeout,
    tags: { name: 'GET /api/v1/wallets/{WALLET_UUID}' },
  });
}

function checkWallet(response, walletId, stage) {
  const body = responseJSON(response);
  return check(response, {
    [`${stage}: HTTP 200`]: (r) => r.status === 200,
    [`${stage}: верный walletId`]: () => body?.walletId === walletId,
    [`${stage}: баланс ${initialBalance}`]: () => body?.balance === initialBalance,
  });
}

function responseJSON(response) {
  try {
    return response.json();
  } catch {
    return null;
  }
}

function positiveInteger(name, fallback) {
  const value = Number(__ENV[name] ?? fallback);
  if (!Number.isSafeInteger(value) || value <= 0) {
    throw new Error(`${name} должен быть положительным целым числом`);
  }
  return value;
}
