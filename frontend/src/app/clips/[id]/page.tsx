import { getVideo } from "@/shared/api";
import Player from "@/shared/player";
import { formatViewsCount } from "@/shared/views-formatter";
import { Suspense } from "react";

async function fetchVideo(id: string) {
  const video = await getVideo(id);
  if (!video) return null;

  return video;
}

export default async function Page({ params }: { params: { id: string } }) {
  const videoData = fetchVideo(params.id);

  const [video] = await Promise.all([videoData]);

  return (
    <main className="max-w-[70%] mt-6 mx-auto">
      <Suspense>
        <Player id={params.id} />
      </Suspense>
      {video && (
        <div className="p-4 flex flex-row">
          <div>
            <h1 className="text-2xl font-bold">{video.title}</h1>
            <p className="text-gray-300">{video.description}</p>
          </div>
          <div className="flex-grow"></div>
          <p className="text-gray-400 text-xl">
            {formatViewsCount(video.views)} view{video.views === 1 ? "" : "s"}
          </p>
        </div>
      )}
    </main>
  );
}
