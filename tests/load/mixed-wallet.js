import http from 'k6/http';
import exec from 'k6/execution';
import { check } from 'k6';
import { Counter } from 'k6/metrics';

const baseURL = (__ENV.BASE_URL || 'http://localhost:8000').replace(/\/$/, '');
const rate = 1000;
const durationSeconds = 60;
const depositAmount = 2;
const withdrawAmount = 1;
const plannedOperations = rate * durationSeconds;

const initialBalance = (plannedOperations + 1) * withdrawAmount;
const requestTimeout = '5s';
const gracefulStopSeconds = 10;

if (![rate, durationSeconds, depositAmount, withdrawAmount, initialBalance,
  initialBalance + (plannedOperations + 1) * depositAmount]
  .every((value) => Number.isSafeInteger(value) && value > 0)) {
  throw new Error('Параметры нагрузки и суммы должны быть положительными безопасными целыми числами');
}

const successfulDeposits = new Counter('successful_deposits');
const successfulWithdrawals = new Counter('successful_withdrawals');
const rejectedOperations = new Counter('rejected_operations');
const unknownOutcomes = new Counter('unknown_outcomes');

export const options = {
  scenarios: {
    mixed_wallet: {
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
    successful_deposits: [`count>=${Math.ceil(plannedOperations / 2)}`],
    successful_withdrawals: [`count>=${Math.floor(plannedOperations / 2)}`],
    rejected_operations: ['count==0'],
    unknown_outcomes: ['count==0'],
    'http_reqs{scenario:mixed_wallet}': [`count>=${plannedOperations}`],
    'http_req_duration{scenario:mixed_wallet}': ['p(95)<200', 'p(99)<500'],
    'http_req_duration{scenario:mixed_wallet,operationType:DEPOSIT}': ['p(95)<200', 'p(99)<500'],
    'http_req_duration{scenario:mixed_wallet,operationType:WITHDRAW}': ['p(95)<200', 'p(99)<500'],
  },
  summaryTrendStats: ['min', 'med', 'p(95)', 'p(99)', 'max'],
};

export function setup() {
  successfulDeposits.add(0);
  successfulWithdrawals.add(0);
  rejectedOperations.add(0);
  unknownOutcomes.add(0);

  const created = http.post(`${baseURL}/api/v1/wallets`, null, {
    timeout: requestTimeout,
    redirects: 0,
    tags: { name: 'create_wallet' },
  });
  const wallet = responseJSON(created);
  if (created.status !== 201 || typeof wallet?.walletId !== 'string' || wallet.balance !== 0) {
    throw new Error(`Не удалось создать пустой кошелёк: HTTP ${created.status}`);
  }

  const payloads = {};
  for (const operationType of ['DEPOSIT', 'WITHDRAW']) {
    payloads[operationType] = JSON.stringify({
      walletId: wallet.walletId,
      operationType,
      amount: operationType === 'DEPOSIT' ? depositAmount : withdrawAmount,
    });
  }

  const funded = http.post(`${baseURL}/api/v1/wallet`, JSON.stringify({
    walletId: wallet.walletId,
    operationType: 'DEPOSIT',
    amount: initialBalance,
  }), {
    headers: { 'Content-Type': 'application/json' },
    timeout: requestTimeout,
    redirects: 0,
    tags: { name: 'fund_wallet' },
  });
  const fundedWallet = responseJSON(funded);
  if (!check(funded, {
    'подготовка: HTTP 200': (r) => r.status === 200,
    'подготовка: верный walletId': () => fundedWallet?.walletId === wallet.walletId,
    'подготовка: исходный баланс': () => fundedWallet?.balance === initialBalance,
  })) {
    throw new Error(`Не удалось пополнить кошелёк: HTTP ${funded.status}`);
  }

  console.log(`walletId=${wallet.walletId}, исходный баланс=${initialBalance}, ` +
    `DEPOSIT=${depositAmount}, WITHDRAW=${withdrawAmount} цент`);
  return { walletId: wallet.walletId, payloads };
}

export default function (data) {
  const operationType = exec.scenario.iterationInInstance % 2 === 0 ? 'DEPOSIT' : 'WITHDRAW';
  const response = http.post(`${baseURL}/api/v1/wallet`, data.payloads[operationType], {
    headers: { 'Content-Type': 'application/json' },
    timeout: requestTimeout,
    redirects: 0,
    tags: { name: 'POST /api/v1/wallet', operationType },
  });

  if (response.status === 200) {
    (operationType === 'DEPOSIT' ? successfulDeposits : successfulWithdrawals).add(1);
  } else if (response.status >= 400 && response.status < 500) {
    rejectedOperations.add(1);
  } else {
    unknownOutcomes.add(1);
  }

  const wallet = responseJSON(response);
  check(response, {
    [`${operationType}: HTTP 200`]: (r) => r.status === 200,
    [`${operationType}: верный walletId`]: () => wallet?.walletId === data.walletId,
    [`${operationType}: неотрицательный целочисленный баланс`]: () =>
      Number.isSafeInteger(wallet?.balance) && wallet.balance >= 0,
  });
}

export function verifyBalance(data) {
  const completed = exec.instance.iterationsCompleted;
  const interrupted = exec.instance.iterationsInterrupted;
  const deposits = Math.ceil(completed / 2);
  const withdrawals = Math.floor(completed / 2);
  const expectedBalance = initialBalance + deposits * depositAmount - withdrawals * withdrawAmount;

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
    'итог: баланс равен исходному плюс пополнения минус списания': () =>
      Number.isSafeInteger(expectedBalance) && wallet?.balance === expectedBalance,
  });

  console.log(`completed=${completed}, interrupted=${interrupted}, ` +
    `ожидаемые успешные DEPOSIT=${deposits}, WITHDRAW=${withdrawals}, ` +
    `ожидаемый баланс при успехе всех POST=${expectedBalance}, фактический=${wallet?.balance}`);
}

function responseJSON(response) {
  try {
    return response.json();
  } catch {
    return null;
  }
}
