ALTER TYPE job_status ADD VALUE IF NOT EXISTS 'recording' BEFORE 'failed';
ALTER TYPE job_status ADD VALUE IF NOT EXISTS 'recorded' BEFORE 'failed';
ALTER TYPE job_status ADD VALUE IF NOT EXISTS 'composing' BEFORE 'failed';
ALTER TYPE job_status ADD VALUE IF NOT EXISTS 'composed' BEFORE 'failed';
ALTER TYPE job_status ADD VALUE IF NOT EXISTS 'done' BEFORE 'failed';
