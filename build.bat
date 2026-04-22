@echo off
echo === Building AI LocalBase ===

echo [1/3] Building frontend...
cd frontend
call npm install
call npm run build
cd ..

echo [2/3] Copying frontend dist to backend...
xcopy /E /Y /I frontend\dist backend\dist

echo [3/3] Building backend exe...
cd backend
go build -o ..\ai-localbase.exe .
cd ..

echo === Build complete: ai-localbase.exe ===
