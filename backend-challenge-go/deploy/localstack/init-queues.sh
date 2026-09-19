#!/bin/bash
# Provisiona as filas exigidas pelo desafio assim que o LocalStack fica pronto.
set -euo pipefail

REGION="${AWS_DEFAULT_REGION:-us-east-1}"
ACCOUNT_ID="000000000000"

DLQ_NAME="wager-transactions-dlq.fifo"
QUEUE_NAME="wager-transactions.fifo"
INTEGRATION_NAME="wagering-integration-events"

awslocal sqs create-queue \
  --queue-name "${DLQ_NAME}" \
  --attributes FifoQueue=true

DLQ_ARN="arn:aws:sqs:${REGION}:${ACCOUNT_ID}:${DLQ_NAME}"

# VisibilityTimeout=30s e maxReceiveCount=5: o processamento síncrono cabe no
# timeout; após 5 receives o broker encaminha à DLQ (ADR-008 / etapa 7).
awslocal sqs create-queue \
  --queue-name "${QUEUE_NAME}" \
  --attributes "{
    \"FifoQueue\": \"true\",
    \"VisibilityTimeout\": \"30\",
    \"RedrivePolicy\": \"{\\\"deadLetterTargetArn\\\":\\\"${DLQ_ARN}\\\",\\\"maxReceiveCount\\\":\\\"5\\\"}\"
  }"

# Destino dos eventos de integração publicados pela outbox (ADR-006).
awslocal sqs create-queue --queue-name "${INTEGRATION_NAME}"

echo "filas provisionadas: ${QUEUE_NAME}, ${DLQ_NAME}, ${INTEGRATION_NAME}"
