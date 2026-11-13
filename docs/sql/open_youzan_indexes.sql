-- Optional indexes for bounded open-API order queries.
-- Review existing indexes and query plans before executing in a maintenance window.
-- This script is not applied automatically by the application.

CREATE INDEX YL_DBS.IX_YZ_ORD_MODIFIED_ID
    ON YL_DBS.DWD_YOUZAN_ORDERS (MODIFIED_DWD, ID);

CREATE INDEX YL_DBS.IX_YZ_ORD_CREATED_ID
    ON YL_DBS.DWD_YOUZAN_ORDERS (CREATED, ID);

CREATE INDEX YL_DBS.IX_YZ_ORD_TID_ID
    ON YL_DBS.DWD_YOUZAN_ORDERS (TID, ID);

CREATE INDEX YL_DBS.IX_YZ_ITEM_MODIFIED_ID
    ON YL_DBS.DWD_YOUZAN_ORDERS_ITEM (MODIFIED_DWD, ID);

CREATE INDEX YL_DBS.IX_YZ_ITEM_CREATED_ID
    ON YL_DBS.DWD_YOUZAN_ORDERS_ITEM (CREATED, ID);
