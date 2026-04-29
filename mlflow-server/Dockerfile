FROM python:3.11-slim

RUN pip install --no-cache-dir mlflow==3.11.1 psycopg2-binary boto3

EXPOSE 8080

CMD mlflow server \
    --host 0.0.0.0 \
    --port 8080 \
    --backend-store-uri $MLFLOW_BACKEND_STORE_URI \
    --artifacts-destination $MLFLOW_ARTIFACTS_DESTINATION \
    --serve-artifacts \
    --workers 1
